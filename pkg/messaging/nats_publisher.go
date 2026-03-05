package messaging

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const (
	maxPublishRetries = 3
)

// Publisher defines the interface for publishing lifecycle events.
type Publisher interface {
	PublishInstanceEvent(eventType string, instance *domain.Instance) error
	GetDefaultVPC(tenantID string) (string, string, error)
	ValidateVPC(tenantID, vpcID string) (bool, error)
	AttachResource(tenantID, instanceID, vpcID string) (string, error)
	DetachResource(tenantID, instanceID, vpcID string) error


	PrepareInstanceNetwork(tenantID, instanceID, vpcID string) (privateIP, gateway, bridgeName string, err error)
ReleaseInstanceNetwork(tenantID, instanceID, vpcID string) error
}

type NATSPublisher struct {
	nc      *nats.Conn
	subject string
}

func NewNATSPublisher(url string, user string, password string, subject string) (*NATSPublisher, error) {
	opts := []nats.Option{
		nats.Name("EC2-Service"),
		nats.Timeout(5 * time.Second),
	}

	if user != "" && password != "" {
		opts = append(opts, nats.UserInfo(user, password))
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", url, err)
	}

	return &NATSPublisher{
		nc:      nc,
		subject: subject,
	}, nil
}

func (p *NATSPublisher) Close() {
	if p.nc != nil {
		p.nc.Close()
	}
}

// PublishInstanceEvent publishes an instance lifecycle event to NATS.
//
// FIX 1: Guard against publishing INSTANCE_STARTED with an empty IP.
//         The network service rejects payloads with no IPAddress, which was
//         causing instances to never get registered and remain unreachable.
//
// FIX 2: Retry with backoff instead of a single fire-and-forget Publish call.
//         A single failed publish silently dropped the registration event.
func (p *NATSPublisher) PublishInstanceEvent(eventType string, instance *domain.Instance) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	// ── FIX 1: Guard — never send INSTANCE_STARTED with an empty IP ──────────
	// Root cause: VM hadn't received an IP yet when the event was published,
	// so the network service received IPAddress="" and rejected with ErrInvalidPayload.
	if eventType == domain.EventInstanceStarted {
		if instance.IP == "" {
			log.Printf("[NATS] [WARN] Skipping INSTANCE_STARTED publish for %s: IP is empty. "+
				"VM may not have received a DHCP lease yet. "+
				"The health monitor will register it once the IP is available.",
				instance.ID)
			return fmt.Errorf("cannot publish INSTANCE_STARTED for %s: IP address is empty", instance.ID)
		}
	}

	correlationID := uuid.New().String()

	event := domain.InstanceLifecycleEvent{
		CorrelationID: correlationID,
		InstanceID:    instance.ID,
		EventType:     eventType,
		Timestamp:     time.Now().Format(time.RFC3339),
		Payload: domain.InstanceLifecyclePayload{
			IPAddress:   instance.IP, // ← was empty before when DHCP hadn't resolved yet
			VPCID:       instance.VPCID,
			ServicePort: 22,
			Metadata: domain.InstanceMetadata{
				InstanceType: "t3.medium",
				AMIID:        instance.Image,
			},
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[NATS] [ERROR] Failed to marshal event for instance %s: %v", instance.ID, err)
		return fmt.Errorf("failed to marshal lifecycle event: %w", err)
	}

	// ── FIX 2: Publish with retry + flush ────────────────────────────────────
	// Previously a single p.nc.Publish() with no flush and no retry meant any
	// transient NATS error silently dropped the registration event entirely.
	var lastErr error
	for attempt := 1; attempt <= maxPublishRetries; attempt++ {
		if err := p.nc.Publish(p.subject, data); err != nil {
			lastErr = err
			log.Printf("[NATS] [WARN] Publish attempt %d/%d failed for %s (%s): %v",
				attempt, maxPublishRetries, instance.ID, eventType, err)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}

		// Flush ensures the message is actually written to the connection buffer
		// before we return. Without this, the message can be lost if the caller
		// exits or the connection closes immediately after Publish.
		if err := p.nc.Flush(); err != nil {
			lastErr = err
			log.Printf("[NATS] [WARN] Flush attempt %d/%d failed for %s: %v",
				attempt, maxPublishRetries, instance.ID, err)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}

		log.Printf("[NATS] [SUCCESS] correlation_id=%s instance_id=%s event_type=%s ip=%s status=published",
			correlationID, instance.ID, eventType, event.Payload.IPAddress)
		return nil
	}

	log.Printf("[NATS] [FAILURE] correlation_id=%s instance_id=%s event_type=%s status=failed error=%v",
		correlationID, instance.ID, eventType, lastErr)
	return fmt.Errorf("failed to publish %s event for %s after %d attempts: %w",
		eventType, instance.ID, maxPublishRetries, lastErr)
}



func (p *NATSPublisher) PrepareInstanceNetwork(tenantID, instanceID, vpcID string) (string, string, string, error) {
	if p == nil || p.nc == nil {
		return "", "", "", fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.instance.prepare"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"instance_id":    instanceID,
		"vpc_id":         vpcID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal prepare network request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s instance_id=%s vpc_id=%s",
		subject, correlationID, tenantID, instanceID, vpcID)

	msg, err := p.nc.Request(subject, data, 5*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] PrepareInstanceNetwork failed: correlation_id=%s error=%v", correlationID, err)
		return "", "", "", fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Success    bool   `json:"success"`
		PrivateIP  string `json:"private_ip"`
		Gateway    string `json:"gateway"`
		BridgeName string `json:"bridge_name"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal prepare network response: %w", err)
	}

	if !response.Success {
		log.Printf("[NATS] [FAILURE] PrepareInstanceNetwork failed: correlation_id=%s error=%s", correlationID, response.Error)
		return "", "", "", fmt.Errorf("network service failed to prepare network: %s", response.Error)
	}

	log.Printf("[NATS] [SUCCESS] Network prepared: correlation_id=%s private_ip=%s gateway=%s bridge=%s",
		correlationID, response.PrivateIP, response.Gateway, response.BridgeName)

	return response.PrivateIP, response.Gateway, response.BridgeName, nil
}

func (p *NATSPublisher) ReleaseInstanceNetwork(tenantID, instanceID, vpcID string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.instance.release"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"instance_id":    instanceID,
		"vpc_id":         vpcID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal release network request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s instance_id=%s vpc_id=%s",
		subject, correlationID, tenantID, instanceID, vpcID)

	msg, err := p.nc.Request(subject, data, 5*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] ReleaseInstanceNetwork failed: correlation_id=%s error=%v", correlationID, err)
		return fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal release network response: %w", err)
	}

	if response.Status != "success" {
		log.Printf("[NATS] [FAILURE] ReleaseInstanceNetwork failed: correlation_id=%s error=%s", correlationID, response.Error)
		return fmt.Errorf("network service failed to release network: %s", response.Error)
	}

	log.Printf("[NATS] [SUCCESS] Network released: correlation_id=%s instance_id=%s", correlationID, instanceID)
	return nil
}


// GetDefaultVPC queries the Network Service for the tenant's default VPC ID and bridge name.
func (p *NATSPublisher) GetDefaultVPC(tenantID string) (string, string, error) {
	if p == nil || p.nc == nil {
		return "", "", fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.vpc.default.get"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal VPC request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s", subject, correlationID, tenantID)

	msg, err := p.nc.Request(subject, data, 2*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] VPC request failed: correlation_id=%s tenant_id=%s error=%v", correlationID, tenantID, err)
		return "", "", fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		VPCID      string `json:"vpc_id"`
		BridgeName string `json:"bridge_name"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		log.Printf("[NATS] [ERROR] Failed to unmarshal VPC response: correlation_id=%s error=%v", correlationID, err)
		return "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.VPCID == "" {
		log.Printf("[NATS] [FAILURE] VPC response empty: correlation_id=%s", correlationID)
		return "", "", fmt.Errorf("received empty VPC ID")
	}

	log.Printf("[NATS] [RESPONSE] correlation_id=%s vpc_id=%s bridge_name=%s status=success", correlationID, response.VPCID, response.BridgeName)
	return response.VPCID, response.BridgeName, nil
}

// ValidateVPC checks if a VPC ID is valid for a given tenant.
func (p *NATSPublisher) ValidateVPC(tenantID, vpcID string) (bool, error) {
	if p == nil || p.nc == nil {
		return false, fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.vpc.validate"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"vpc_id":         vpcID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return false, fmt.Errorf("failed to marshal VPC validation request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s vpc_id=%s", subject, correlationID, tenantID, vpcID)

	msg, err := p.nc.Request(subject, data, 2*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] VPC validation request failed: correlation_id=%s error=%v", correlationID, err)
		return false, fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return false, fmt.Errorf("failed to unmarshal validation response: %w", err)
	}

	log.Printf("[NATS] [RESPONSE] correlation_id=%s valid=%v status=success", correlationID, response.Valid)
	return response.Valid, nil
}

// AttachResource sends a request to the Network Service to attach an instance to a VPC subnet and returns the allocated private IP.
func (p *NATSPublisher) AttachResource(tenantID, instanceID, vpcID string) (string, error) {
	if p == nil || p.nc == nil {
		return "", fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.resource.attach"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"resource_arn":   fmt.Sprintf("arn:aws:ec2:::%s", instanceID),
		"target_vpc_id":  vpcID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal attach request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s instance_id=%s target_vpc_id=%s", subject, correlationID, tenantID, instanceID, vpcID)

	msg, err := p.nc.Request(subject, data, 2*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] Attach request failed: correlation_id=%s error=%v", correlationID, err)
		return "", fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Success   bool   `json:"success"`
		PrivateIP string `json:"private_ip"`
		Error     string `json:"error"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal attach response: %w", err)
	}

	if !response.Success {
		log.Printf("[NATS] [FAILURE] Attach failed: correlation_id=%s error=%s", correlationID, response.Error)
		log.Printf("[NATS] [WARNING] Instance will proceed without Network Service registration")
		return "", nil
	}

	log.Printf("[NATS] [SUCCESS] Resource attached: correlation_id=%s private_ip=%s", correlationID, response.PrivateIP)
	return response.PrivateIP, nil
}

// DetachResource sends a request to the Network Service to detach an instance from a VPC.
func (p *NATSPublisher) DetachResource(tenantID, instanceID, vpcID string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.resource.detach"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"resource_arn":   fmt.Sprintf("arn:aws:ec2:::%s", instanceID),
		"vpc_id":         vpcID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal detach request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s instance_id=%s", subject, correlationID, tenantID, instanceID)

	msg, err := p.nc.Request(subject, data, 2*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] Detach request failed: correlation_id=%s error=%v", correlationID, err)
		return fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal detach response: %w", err)
	}

	if response.Status != "success" {
		log.Printf("[NATS] [FAILURE] Detach failed: correlation_id=%s error=%s", correlationID, response.Error)
		return fmt.Errorf("Network Service detachment failed: %s", response.Error)
	}

	log.Printf("[NATS] [SUCCESS] Resource detached: correlation_id=%s", correlationID)
	return nil
}
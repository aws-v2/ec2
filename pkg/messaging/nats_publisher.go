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

// Publisher defines the interface for publishing lifecycle events.
type Publisher interface {
	PublishInstanceEvent(eventType string, instance *domain.Instance) error
	GetDefaultVPC(tenantID string) (string, error)
	ValidateVPC(tenantID, vpcID string) (bool, error)
	AttachResource(tenantID, instanceID, vpcID string) error
	DetachResource(tenantID, instanceID, vpcID string) error
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
// It is non-blocking in the sense that NATS Publish is asynchronous, 
// and we log any errors without failing the caller.
func (p *NATSPublisher) PublishInstanceEvent(eventType string, instance *domain.Instance) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}
	correlationID := uuid.New().String()
	
	event := domain.InstanceLifecycleEvent{
		CorrelationID: correlationID,
		InstanceID:    instance.ID,
		EventType:     eventType,
		Timestamp:     time.Now().Format(time.RFC3339),
		Payload: domain.InstanceLifecyclePayload{
			IPAddress:   instance.IP,
			VPCID:       instance.VPCID,
			ServicePort: 22, // Default SSH port for now
			Metadata: domain.InstanceMetadata{
				InstanceType: "t3.medium", // Mocked for now as we don't have this in domain model yet
				AMIID:        instance.Image,
			},
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[NATS] [ERROR] Failed to marshal event for instance %s: %v", instance.ID, err)
		return err
	}

	// Logging requirement: correlation_id, instance_id, event_type, publish status
	err = p.nc.Publish(p.subject, data)
	if err != nil {
		log.Printf("[NATS] [FAILURE] correlation_id=%s instance_id=%s event_type=%s status=failed error=%v", 
			correlationID, instance.ID, eventType, err)
		return err
	}

	log.Printf("[NATS] [SUCCESS] correlation_id=%s instance_id=%s event_type=%s status=success", 
		correlationID, instance.ID, eventType)
	
	return nil
}

// GetDefaultVPC queries the Network Service for the tenant's default VPC ID.
func (p *NATSPublisher) GetDefaultVPC(tenantID string) (string, error) {
	if p == nil || p.nc == nil {
		return "", fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.vpc.default.get"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal VPC request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s", subject, correlationID, tenantID)

	msg, err := p.nc.Request(subject, data, 2*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] VPC request failed: correlation_id=%s tenant_id=%s error=%v", correlationID, tenantID, err)
		return "", fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		VPCID string `json:"vpc_id"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		log.Printf("[NATS] [ERROR] Failed to unmarshal VPC response: correlation_id=%s error=%v", correlationID, err)
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.VPCID == "" {
		log.Printf("[NATS] [FAILURE] VPC response empty: correlation_id=%s", correlationID)
		return "", fmt.Errorf("received empty VPC ID")
	}

	log.Printf("[NATS] [RESPONSE] correlation_id=%s vpc_id=%s status=success", correlationID, response.VPCID)
	return response.VPCID, nil
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

// AttachResource sends a request to the Network Service to attach an instance to a VPC.
func (p *NATSPublisher) AttachResource(tenantID, instanceID, vpcID string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := "dev.network.v1.resource.attach"

	request := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"resource_arn":   fmt.Sprintf("arn:aws:ec2:::%s", instanceID), // Simulated ARN
		"target_vpc_id":  vpcID,
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal attach request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s instance_id=%s target_vpc_id=%s", subject, correlationID, tenantID, instanceID, vpcID)

	msg, err := p.nc.Request(subject, data, 2*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] Attach request failed: correlation_id=%s error=%v", correlationID, err)
		return fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal attach response: %w", err)
	}

	if response.Status != "success" {
		log.Printf("[NATS] [FAILURE] Attach failed: correlation_id=%s error=%s", correlationID, response.Error)
		return fmt.Errorf("Network Service attachment failed: %s", response.Error)
	}

	log.Printf("[NATS] [SUCCESS] Resource attached: correlation_id=%s", correlationID)
	return nil
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

// Why this is being implemented: 
// This publisher allows the Network Service to learn about EC2 instances and track their health, 
// enabling future integration of VPC/subnet awareness safely without breaking existing libvirt logic.

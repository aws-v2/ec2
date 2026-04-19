package messaging

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"ec2-api/internal/domain"

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
	ListVPCs(tenantID string) ([]domain.VPC, error)
	CreateVPC(tenantID, vpcName, requestedBy string) error

	PrepareInstanceNetwork(tenantID, instanceID, vpcID string) (privateIP, gateway, bridgeName string, err error)
	ReleaseInstanceNetwork(tenantID, instanceID, vpcID string) error
	RequestInstanceToken(userID, instanceID string) (string, error)
	PublishScalingPolicy(tenantID string, policy domain.ScalingPolicyRequest) error
	GetScalingPolicies(tenantID string) ([]domain.ScalingPolicy, error)
	UpdateScalingPolicy(tenantID, policyID string, req domain.UpdateScalingPolicyRequest) error
	DeleteScalingPolicy(tenantID, policyID string) error
	PublishProvisioningProgress(instanceID, stage, message string) error
}

type NATSPublisher struct {
	nc      *nats.Conn
	profile string
	subject string // default/base subject
}

// BuildSubject constructs a NATS subject following the pattern:
// <profile>.<service>.<version>.<domain>.<action>
func BuildSubject(profile, domain, action string) string {
	return fmt.Sprintf("%s.ec2.v1.%s.%s", profile, domain, action)
}

func NewNATSPublisher(url string, user string, password string, profile string) (*NATSPublisher, error) {
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
		profile: profile,
		subject: BuildSubject(profile, "instance", "lifecycle"),
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
//
//	The network service rejects payloads with no IPAddress, which was
//	causing instances to never get registered and remain unreachable.
//
// FIX 2: Retry with backoff instead of a single fire-and-forget Publish call.
//
//	A single failed publish silently dropped the registration event.
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
	subject := fmt.Sprintf("%s.network.v1.instance.prepare", p.profile)

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
	subject := fmt.Sprintf("%s.network.v1.instance.release", p.profile)

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
	subject := fmt.Sprintf("%s.network.v1.vpc.default.get", p.profile)

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
	subject := fmt.Sprintf("%s.network.v1.vpc.validate", p.profile)

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
	subject := fmt.Sprintf("%s.network.v1.resource.attach", p.profile)

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

func (p *NATSPublisher) DetachResource(tenantID, instanceID, vpcID string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := fmt.Sprintf("%s.network.v1.resource.detach", p.profile)

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

type listVPCsRequest struct {
	CorrelationID string `json:"correlation_id"`
	TenantID      string `json:"tenant_id"`
}

type listVPCsResponse struct {
	VPCs  []domain.VPC `json:"vpcs"`
	Error string       `json:"error"`
}

type createVPCEvent struct {
	CorrelationID string `json:"correlation_id"`
	TenantID      string `json:"tenant_id"`
	VPCName       string `json:"vpc_name"`
	RequestedBy   string `json:"requested_by"`
}

// ListVPCs requests the list of VPCs for a tenant from the Network Service via NATS.
func (p *NATSPublisher) ListVPCs(tenantID string) ([]domain.VPC, error) {
	if p == nil || p.nc == nil {
		return nil, fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	req := listVPCsRequest{
		CorrelationID: correlationID,
		TenantID:      tenantID,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	log.Printf("[RDS-NATS] Sending ListVPCs request for tenant %s (correlation_id=%s)", tenantID, correlationID)

	subject := fmt.Sprintf("%s.network.v1.vpc.list", p.profile)
	msg, err := p.nc.Request(subject, reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("NATS request failed: %w", err)
	}

	var resp listVPCsResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("failed to list VPCs: %s", resp.Error)
	}

	log.Printf("[NATS] Successfully retrieved %d VPCs for tenant %s", len(resp.VPCs), tenantID)
	return resp.VPCs, nil
}

// CreateVPC dispatches an asynchronous NATS message to create a new VPC.
func (p *NATSPublisher) CreateVPC(tenantID, vpcName, requestedBy string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	event := createVPCEvent{
		CorrelationID: correlationID,
		TenantID:      tenantID,
		VPCName:       vpcName,
		RequestedBy:   requestedBy,
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal create VPC event: %w", err)
	}

	log.Printf("[RDS-NATS] Publishing CreateVPC event for tenant %s, vpc %s (correlation_id=%s)", tenantID, vpcName, correlationID)
	subject := fmt.Sprintf("%s.network.v1.vpc.create", p.profile)
	if err := p.nc.Publish(subject, eventData); err != nil {
		return fmt.Errorf("failed to publish create VPC event: %w", err)
	}

	return nil
}

// ── IAM Token Request ────────────────────────────────────────────────────────

type InstanceTokenRequest struct {
	InstanceID string `json:"instance_id"`
	UserID     string `json:"user_id"`
}

type InstanceTokenResponse struct {
	Token string `json:"token"`
	Error string `json:"error,omitempty"`
}

// RequestInstanceToken asks the IAM service for a scoped JWT token that will be
// injected into the VM via cloud-init so the metrics agent can authenticate.
func (p *NATSPublisher) RequestInstanceToken(userID, instanceID string) (string, error) {
	if p == nil || p.nc == nil {
		return "", fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := fmt.Sprintf("%s.iam.v1.token.generate", p.profile)

	req := InstanceTokenRequest{
		InstanceID: instanceID,
		UserID:     userID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal instance token request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s user_id=%s instance_id=%s",
		subject, correlationID, userID, instanceID)

	msg, err := p.nc.Request(subject, data, 5*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] RequestInstanceToken failed: correlation_id=%s error=%v", correlationID, err)
		return "", fmt.Errorf("NATS request failed: %w", err)
	}

	var resp InstanceTokenResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal instance token response: %w", err)
	}

	if resp.Error != "" {
		log.Printf("[NATS] [FAILURE] RequestInstanceToken: correlation_id=%s error=%s", correlationID, resp.Error)
		return "", fmt.Errorf("IAM service error: %s", resp.Error)
	}

	if resp.Token == "" {
		log.Printf("[NATS] [FAILURE] RequestInstanceToken: correlation_id=%s error=empty_token", correlationID)
		return "", fmt.Errorf("IAM service returned an empty token")
	}

	log.Printf("[NATS] [SUCCESS] Instance token received: correlation_id=%s instance_id=%s", correlationID, instanceID)
	return resp.Token, nil
}

// PublishScalingPolicy publishes a scaling policy creation event to the metrics service.
func (p *NATSPublisher) PublishScalingPolicy(tenantID string, policy domain.ScalingPolicyRequest) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := fmt.Sprintf("%s.metrics.v1.scaling_policy.create", p.profile)

	event := map[string]interface{}{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"policy":         policy,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal scaling policy event: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s target_id=%s",
		subject, correlationID, tenantID, policy.TargetID)

	if err := p.nc.Publish(subject, data); err != nil {
		log.Printf("[NATS] [ERROR] Failed to publish scaling policy event: correlation_id=%s error=%v", correlationID, err)
		return fmt.Errorf("failed to publish scaling policy event: %w", err)
	}

	log.Printf("[NATS] [SUCCESS] Published scaling policy event: correlation_id=%s target_id=%s", correlationID, policy.TargetID)
	return nil
}

// GetScalingPolicies requests the scaling policies for a tenant from the metrics service.
func (p *NATSPublisher) GetScalingPolicies(tenantID string) ([]domain.ScalingPolicy, error) {
	if p == nil || p.nc == nil {
		return nil, fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := fmt.Sprintf("%s.metrics.v1.scaling_policy.list", p.profile)

	req := map[string]string{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal get policies request: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s", subject, correlationID, tenantID)

	msg, err := p.nc.Request(subject, data, 5*time.Second)
	if err != nil {
		log.Printf("[NATS] [ERROR] GetScalingPolicies failed: correlation_id=%s error=%v", correlationID, err)
		return nil, fmt.Errorf("NATS request failed: %w", err)
	}

	var response struct {
		Policies []domain.ScalingPolicy `json:"policies"`
		Error    string                 `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal policies response: %w", err)
	}

	if response.Error != "" {
		log.Printf("[NATS] [FAILURE] GetScalingPolicies: correlation_id=%s error=%s", correlationID, response.Error)
		return nil, fmt.Errorf("metrics service error: %s", response.Error)
	}

	log.Printf("[NATS] [SUCCESS] Retrieved %d policies: correlation_id=%s", len(response.Policies), correlationID)
	log.Printf("[NATS] [SUCCESS] Policies: %v", response.Policies)
	return response.Policies, nil
}

// UpdateScalingPolicy publishes an update event for a scaling policy.
func (p *NATSPublisher) UpdateScalingPolicy(tenantID, policyID string, req domain.UpdateScalingPolicyRequest) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := fmt.Sprintf("%s.metrics.v1.scaling_policy.update", p.profile)

	event := map[string]interface{}{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"policy_id":      policyID,
		"update":         req,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal update policy event: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s policy_id=%s",
		subject, correlationID, tenantID, policyID)

	if err := p.nc.Publish(subject, data); err != nil {
		log.Printf("[NATS] [ERROR] Failed to publish update policy event: correlation_id=%s error=%v", correlationID, err)
		return fmt.Errorf("failed to publish update scaling policy event: %w", err)
	}

	log.Printf("[NATS] [SUCCESS] Published update policy event: correlation_id=%s policy_id=%s", correlationID, policyID)
	return nil
}

// DeleteScalingPolicy publishes a delete event for a scaling policy.
func (p *NATSPublisher) DeleteScalingPolicy(tenantID, policyID string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	correlationID := uuid.New().String()
	subject := fmt.Sprintf("%s.metrics.v1.scaling_policy.delete", p.profile)

	event := map[string]interface{}{
		"correlation_id": correlationID,
		"tenant_id":      tenantID,
		"policy_id":      policyID,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal delete policy event: %w", err)
	}

	log.Printf("[NATS] [REQUEST] subject=%s correlation_id=%s tenant_id=%s policy_id=%s",
		subject, correlationID, tenantID, policyID)

	if err := p.nc.Publish(subject, data); err != nil {
		log.Printf("[NATS] [ERROR] Failed to publish delete policy event: correlation_id=%s error=%v", correlationID, err)
		return fmt.Errorf("failed to publish delete scaling policy event: %w", err)
	}

	log.Printf("[NATS] [SUCCESS] Published delete policy event: correlation_id=%s policy_id=%s", correlationID, policyID)
	return nil
}

func (p *NATSPublisher) PublishProvisioningProgress(instanceID, stage, message string) error {
	if p == nil || p.nc == nil {
		return fmt.Errorf("NATS publisher or connection not initialized")
	}

	event := domain.ProvisioningProgressEvent{
		InstanceID: instanceID,
		EventType:  domain.EventProvisioningProgress,
		Stage:      stage,
		Message:    message,
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal progress event: %w", err)
	}

	// Use the same subject for now, as the backend will filter by event_type
	if err := p.nc.Publish(p.subject, data); err != nil {
		return fmt.Errorf("failed to publish progress event: %w", err)
	}

	return p.nc.Flush()
}

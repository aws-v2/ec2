package application

import (
	"context"
	"fmt"
	"log"
	domain "ec2-api/internal/domain/instance"

)

func (s *InstanceService) CreateScalingPolicy(ctx context.Context, userID string, req *domain.ScalingPolicyRequest) error {
	return s.repo.CreateScalingPolicy(ctx, userID, req)
}

func (s *InstanceService) GetScalingPolicies(ctx context.Context, userID string) ([]domain.ScalingPolicy, error) {
	return s.repo.GetScalingPolicies(ctx, userID)
}

func (s *InstanceService) UpdateScalingPolicy(ctx context.Context, userID, policyID string, req *domain.UpdateScalingPolicyRequest) error {
	return s.repo.UpdateScalingPolicy(ctx, userID, policyID, req)
}

func (s *InstanceService) DeleteScalingPolicy(ctx context.Context, userID, policyID string) error {
	return s.repo.DeleteScalingPolicy(ctx, userID, policyID)
}
// ── Scaling Enforcement ──────────────────────────────────────────────────

// EnforceScaling receives a requested scale action from the Metrics Service and attempts to execute it.
func (s *InstanceService) EnforceScaling(ctx context.Context, event *domain.ScaleEvent) error {
	log.Printf("[SCALER] Enforcing scale action: %s for target: %s (tenant: %s)", event.Action, event.Policy.TargetID, event.Policy.UserID)

	// In the future, target_type could be "asg", and we'd look up the ASG details here.
	// For now, if the target is an individual instance, we act on that instance directly.
	if event.Policy.PolicyType != "instance" {
		return fmt.Errorf("unsupported target_type: %s", event.Policy.PolicyType)
	}

	targetInstance, err := s.GetInstance(event.Policy.TargetID, event.UserID)
	if err != nil {
		return fmt.Errorf("failed to fetch target instance %s: %w", event.Policy.TargetID, err)
	}

	switch event.Action {
	case domain.ScaleOutAction:
		return s.handleScaleOut(ctx,targetInstance, event.Policy.MaxCapacity)
	case domain.ScaleInAction:
		return s.handleScaleIn(targetInstance)
	default:
		return fmt.Errorf("unknown scale action: %s", event.Action)
	}
}

func (s *InstanceService) handleScaleOut(ctx context.Context,baseInstance *domain.Instance, maxInstances int) error {
	log.Printf("[SCALER] Initiating scale-out based on instance %s (VPC: %s)", baseInstance.ID, baseInstance.VPCID)
	
	// Check if max limit is reached
	if maxInstances > 0 {
		instances, err := s.ListInstances(baseInstance.UserID)
		if err != nil {
			return fmt.Errorf("failed to list instances to enforce scale limit: %w", err)
		}

		currentCount := 0
		for _, inst := range instances {
			if inst.Status == domain.StatusRunning || inst.Status == domain.StatusPending {
				if inst.VPCID == baseInstance.VPCID && inst.Image == baseInstance.Image {
					currentCount++
				}
			} 
		}

		if currentCount >= maxInstances {
			log.Printf("[SCALER] [WARNING] Scale-out blocked. Current instances (%d) reached or exceeded max limit (%d)", currentCount, maxInstances)
			return nil
		}
	}

	req := &domain.CreateInstanceRequest{
		Image:  baseInstance.Image,
		CPU:    baseInstance.CPU,
		RAM:    baseInstance.RAM,
	}

	_, err := s.CreateInstance(ctx,req, baseInstance.UserID)
	return err
}

func (s *InstanceService) handleScaleIn(baseInstance *domain.Instance) error {
	log.Printf("[SCALER] Initiating scale-in for target instance %s", baseInstance.ID)

	instances, err := s.ListInstances(baseInstance.UserID)
	if err != nil {
		return fmt.Errorf("failed to list instances for scale-in: %w", err)
	}

	var activeReplicas []*domain.Instance
	for _, inst := range instances {
		if (inst.Status == domain.StatusRunning || inst.Status == domain.StatusPending) && inst.ID != baseInstance.ID {
			if inst.VPCID == baseInstance.VPCID && inst.Image == baseInstance.Image {
				activeReplicas = append(activeReplicas, inst)
			}
		}
	}

	if len(activeReplicas) == 0 {
		log.Printf("[SCALER] [WARNING] Scale-in blocked. No replicas to terminate.")
		return nil
	}

	targetToTerminate := activeReplicas[len(activeReplicas)-1]
	return s.DeleteInstance(targetToTerminate.ID, targetToTerminate.UserID)
}






// HandleProvision handles a request to provision a new VM based on a profile.
func (s *InstanceService) HandleProvision(ctx context.Context, event *domain.ProvisionInstanceEvent) error {
	log.Printf("[PROVISIONER] Provisioning VM for profile: %s", event.Profile)

	// Merge flat fields into Parameters for backward compatibility
	if event.Parameters == nil {
		event.Parameters = make(map[string]string)
	}
	if event.StorageARN != "" && event.Parameters["STORAGE_ARN"] == "" {
		event.Parameters["STORAGE_ARN"] = event.StorageARN
	}
	if event.HeadlessBin != "" && event.Parameters["HEADLESS_BIN"] == "" {
		event.Parameters["HEADLESS_BIN"] = event.HeadlessBin
	}

	// Create a provision request
	req := &domain.CreateInstanceRequest{
		Image:      "ubuntu-22.04", // Default base image
		CPU:        event.Specs.CPU,
		RAM:        event.Specs.RAM,
		Profile:    event.Profile,
		Parameters: event.Parameters,
	}

	// Ensure sensible defaults if specs are empty
	if req.CPU == 0 {
		req.CPU = 2
	}
	if req.RAM == 0 {
		req.RAM = 4096
	}

	// Use the provided user ID or "system"
	// userID := event.UserID  4c8eeb08-97d2-4c1e-ae0c-e37675b49ee2
	// if userID == "" {
	// 	userID = "system"
	// }

	userID := "4c8eeb08-97d2-4c1e-ae0c-e37675b49ee2"


	_, err := s.CreateInstance(ctx,req, userID)
	if err != nil {
		return fmt.Errorf("failed to create instance for profile %s: %w", event.Profile, err)
	}

	return nil
}

func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

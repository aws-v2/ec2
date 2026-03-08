package domain

type ScalingPolicyRequest struct {
	TargetType       string  `json:"target_type"` // e.g., "instance", "asg"
	TargetID         string  `json:"target_id" binding:"required"`
	MetricName       string  `json:"metric_name" binding:"required"`
	TargetValue      float64 `json:"target_value" binding:"required"`
	ScaleDownValue   float64 `json:"scale_down_value"`
	MaxInstances     int     `json:"max_instances"`
	ScaleOutCooldown int     `json:"scale_out_cooldown"`
	ScaleInCooldown  int     `json:"scale_in_cooldown"`
}

type UpdateScalingPolicyRequest struct {
	TargetValue      float64 `json:"target_value"`
	ScaleDownValue   float64 `json:"scale_down_value"`
	MaxInstances     int     `json:"max_instances"`
	ScaleOutCooldown int     `json:"scale_out_cooldown"`
	ScaleInCooldown  int     `json:"scale_in_cooldown"`
}

type ScalingPolicy struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	TargetType       string  `json:"target_type"`
	TargetID         string  `json:"target_id"`
	MetricName       string  `json:"metric_name"`
	TargetValue      float64 `json:"target_value"`
	ScaleDownValue   float64 `json:"scale_down_value"`
	MaxInstances     int     `json:"max_instances"`
	ScaleOutCooldown int     `json:"scale_out_cooldown"`
	ScaleInCooldown  int     `json:"scale_in_cooldown"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type ScaleAction string

const (
	ScaleOutAction ScaleAction = "scale_out"
	ScaleInAction  ScaleAction = "scale_in"
)

// ScaleEvent is the payload sent by the Metrics Service when a policy is breached.
type ScaleEvent struct {
	CorrelationID string               `json:"correlation_id"`
	TenantID      string               `json:"tenant_id"`
	Action        ScaleAction          `json:"action"`
	Policy        ScalingPolicyRequest `json:"policy"`
	CurrentValue  float64              `json:"current_value"`
}

package domain

type ScalingPolicyRequest struct {
	TargetType       string  `json:"target_type"` // e.g., "instance", "asg"
	TargetID         string  `json:"target_id" binding:"required"`
	MetricName       string  `json:"metric_name" binding:"required"`
	TargetValue      float64 `json:"target_value" binding:"required"`
	ScaleOutCooldown int     `json:"scale_out_cooldown"`
	ScaleInCooldown  int     `json:"scale_in_cooldown"`
}

type UpdateScalingPolicyRequest struct {
	TargetValue      float64 `json:"target_value"`
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
	ScaleOutCooldown int     `json:"scale_out_cooldown"`
	ScaleInCooldown  int     `json:"scale_in_cooldown"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

package domain

import "time"

type ScalingPolicyRequest struct {
	Name             string  `json:"name" binding:"required"`
	PolicyType       string  `json:"policy_type" binding:"required"`
	MinCapacity      int     `json:"min_capacity"`
	MaxCapacity      int     `json:"max_capacity"`
	TargetValue      float64 `json:"target_value" binding:"required"`
	ScaleInCooldown  int     `json:"scale_in_cooldown"`
	ScaleOutCooldown int     `json:"scale_out_cooldown"`
	TargetID         string  `json:"target_id"`
	UserID           string  `json:"user_id"`
}

type UpdateScalingPolicyRequest struct {
	Name             *string  `json:"name,omitempty"`
	MinCapacity      *int     `json:"min_capacity,omitempty"`
	MaxCapacity      *int     `json:"max_capacity,omitempty"`
	TargetValue      *float64 `json:"target_value,omitempty"`
	ScaleInCooldown  *int     `json:"scale_in_cooldown,omitempty"`
	ScaleOutCooldown *int     `json:"scale_out_cooldown,omitempty"`

	TargetType     *string  `json:"target_type,omitempty"`
	TargetID       *string  `json:"target_id,omitempty"`
	MetricName     *string  `json:"metric_name,omitempty"`
	ScaleDownValue *float64 `json:"scale_down_value,omitempty"`
	MaxInstances   *int     `json:"max_instances,omitempty"`
}
type ScalingPolicy struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Name             string    `json:"name"`
	PolicyType       string    `json:"policy_type"`
	MinCapacity      int       `json:"min_capacity"`
	MaxCapacity      int       `json:"max_capacity"`
	TargetValue      float64   `json:"target_value"`
	ScaleInCooldown  int       `json:"scale_in_cooldown"`
	ScaleOutCooldown int       `json:"scale_out_cooldown"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	TargetID         string    `json:"target_id"`
}

type ScaleAction string

const (
	ScaleOutAction ScaleAction = "scale_out"
	ScaleInAction  ScaleAction = "scale_in"
)

type ScaleEvent struct {
	CorrelationID string               `json:"correlation_id"`
	UserID        string               `json:"user_id"`
	Action        ScaleAction          `json:"action"`
	Policy        ScalingPolicyRequest `json:"policy"`
	CurrentValue  float64              `json:"current_value"`
}

type EC2Response struct {
	GatewayIP   string `json:"gateway_ip"`
	GatewayPort int    `json:"gateway_port"`
	VMiP        string `json:"vm_ip"`
	VMPORT      int    `json:"vm_port"`
}

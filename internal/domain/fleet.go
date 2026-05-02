package domain

import "time"

// FleetOverview represents high-level metrics across compute resources
type FleetOverview struct {
	TotalInstances   int     `json:"totalInstances"`
	ActiveInstances  int     `json:"activeInstances"`
	AvgCpuLoad       float64 `json:"avgCpuLoad"`
	AvgRamUsage      float64 `json:"avgRamUsage"`
	ActiveFunctions  int     `json:"activeFunctions"`
	ClusterHealth    string  `json:"clusterHealth"`
}

// FleetEventType represents the type of a fleet event
type FleetEventType string

const (
	EventInfo    FleetEventType = "info"
	EventWarn    FleetEventType = "warn"
	EventError   FleetEventType = "error"
	EventSuccess FleetEventType = "success"
)

// FleetEvent represents a system event for the fleet
type FleetEvent struct {
	ID        string         `json:"id" db:"id"`
	Timestamp time.Time      `json:"timestamp" db:"timestamp"`
	Type      FleetEventType `json:"type" db:"type"`
	Message   string         `json:"message" db:"message"`
	Resource  string         `json:"resource" db:"resource"`
	UserID    string         `json:"user_id" db:"user_id"`
}

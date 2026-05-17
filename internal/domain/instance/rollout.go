package domain

import (
	"time"

	"github.com/google/uuid"
)


type RolloutUpdateRequest struct {
    UserID   string 
    Version  string
    FileName string
    SHA256   string
}

type AgentUpdatePayload struct {
    Version string
    URL     string
    SHA256  string
}

type UpdateResult struct {
    HostID string 
    Addr   string
    OK     bool
    Error  string
}

type RolloutSummary struct {
    RolloutID string 
    Version   string
    Total     int
    OK        int
    Failed    int
    S3Error   string
    Results   []UpdateResult
}

// Stored in agent_rollouts
type AgentRollout struct {
    ID          uuid.UUID 
    Version     string
    FileName    string
    SHA256      string
    URL         string
    InitiatedBy string 
    Total       int
    OK          int
    Failed      int
    S3Error     string
    CreatedAt   time.Time
}

// Stored in agent_update_statuses
type AgentUpdateStatus struct {
    ID        uuid.UUID 
    RolloutID uuid.UUID 
    HostID    string
    HostIP    string
    Status    string // pending | complete | failed
    ErrorMsg  string
    SentAt    time.Time
    UpdatedAt time.Time
}
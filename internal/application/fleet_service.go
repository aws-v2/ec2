package application

import (
	"fmt"
	"time"

	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"
)

type FleetService struct {
	fleetRepo    interfaces.FleetRepository
	instanceRepo interfaces.InstanceRepository
}

func NewFleetService(fleetRepo interfaces.FleetRepository, instanceRepo interfaces.InstanceRepository) *FleetService {
	return &FleetService{
		fleetRepo:    fleetRepo,
		instanceRepo: instanceRepo,
	}
}

func (s *FleetService) GetOverview(userID string) (*domain.FleetOverview, error) {
	overview, err := s.fleetRepo.GetOverview(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fleet overview: %w", err)
	}

	return overview, nil
}

func (s *FleetService) GetEvents(userID string) ([]*domain.FleetEvent, error) {
	return s.fleetRepo.GetEvents(userID, 10)
}

func (s *FleetService) LogEvent(userID, eventType, message, resource string) error {
	event := &domain.FleetEvent{
		ID:        fmt.Sprintf("evt_%d", SystemTimeNow().UnixNano()),
		Timestamp: SystemTimeNow(),
		Type:      domain.FleetEventType(eventType),
		Message:   message,
		Resource:  resource,
		UserID:    userID,
	}
	return s.fleetRepo.LogEvent(event)
}

var SystemTimeNow = func() time.Time {
	return time.Now()
}

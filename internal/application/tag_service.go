package application

import (
	domain "ec2-api/internal/domain/instance"

)

func (s *InstanceService) GetTags(id, userID string) ([]*domain.InstanceTag, error) {
	if _, err := s.GetInstance(id, userID); err != nil {
		return nil, err
	}
	return s.repo.GetTags(id)
}

func (s *InstanceService) AddOrUpdateTag(id, userID string, tag *domain.InstanceTag) error {
	if _, err := s.GetInstance(id, userID); err != nil {
		return err
	}
	return s.repo.AddOrUpdateTag(id, tag)
}

func (s *InstanceService) DeleteTag(id, userID string, key string) error {
	if _, err := s.GetInstance(id, userID); err != nil {
		return err
	}
	return s.repo.DeleteTag(id, key)
}
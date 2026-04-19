package tests

import (
	"os"
	"testing"

	"ec2-api/internal/config"
	"ec2-api/internal/domain"
)

func TestConfigDefaults(t *testing.T) {
	// Ensure environment is clean for default test
	os.Clearenv()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test some defaults from internal/config/config.go
	if cfg.Server.Port != "8088" {
		t.Errorf("Expected default server port 8088, got %s", cfg.Server.Port)
	}

	if cfg.DB.Port != 5432 {
		t.Errorf("Expected default DB port 5432, got %d", cfg.DB.Port)
	}

	if cfg.Libvirt.URI != "qemu:///system" {
		t.Errorf("Expected default Libvirt URI qemu:///system, got %s", cfg.Libvirt.URI)
	}
}

func TestInstanceDomainModel(t *testing.T) {
	// Verify constants
	if domain.StatusRunning != "running" {
		t.Errorf("Expected StatusRunning to be 'running', got %s", domain.StatusRunning)
	}

	if domain.EventInstanceStarted != "INSTANCE_STARTED" {
		t.Errorf("Expected EventInstanceStarted to be 'INSTANCE_STARTED', got %s", domain.EventInstanceStarted)
	}

	// Structural check (implicitly checks fields exist and are of correct type)
	instance := domain.Instance{
		ID:     "i-123456",
		Status: domain.StatusRunning,
		CPU:    2,
		RAM:    2048,
	}

	if instance.ID != "i-123456" || instance.Status != "running" {
		t.Error("Instance struct integrity check failed")
	}
}

func TestLifecycleEventStructure(t *testing.T) {
	event := domain.InstanceLifecycleEvent{
		InstanceID: "i-abc",
		EventType:  domain.EventInstanceStarted,
		Payload: domain.InstanceLifecyclePayload{
			IPAddress: "192.168.1.10",
		},
	}

	if event.EventType != "INSTANCE_STARTED" {
		t.Errorf("Event type mismatch: %s", event.EventType)
	}

	if event.Payload.IPAddress != "192.168.1.10" {
		t.Errorf("Payload IP mismatch: %s", event.Payload.IPAddress)
	}
}

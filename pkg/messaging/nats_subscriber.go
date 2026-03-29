package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/nats-io/nats.go"
)

// EC2EventHandler is the interface the subscriber expects to call when events occur.
type EC2EventHandler interface {
	EnforceScaling(ctx context.Context, event *domain.ScaleEvent) error
	HandleVMProvision(ctx context.Context, event *domain.GameVMProvisionEvent) error
}

type NATSSubscriber struct {
	nc       *nats.Conn
	handler  EC2EventHandler
}

func NewNATSSubscriber(url, user, password string, handler EC2EventHandler) (*NATSSubscriber, error) {
	opts := []nats.Option{
		nats.Name("EC2-Subscriber"),
	}
	if user != "" && password != "" {
		opts = append(opts, nats.UserInfo(user, password))
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS for subscriber: %w", err)
	}

	return &NATSSubscriber{
		nc:      nc,
		handler: handler,
	}, nil
}

func (s *NATSSubscriber) Start() error {
	// 1. Subscribe to Scaling Events
	scaleSubject := "dev.ec2.v1.scale.*"
	queueGroup := "ec2-enforcers"

	_, err := s.nc.QueueSubscribe(scaleSubject, queueGroup, func(msg *nats.Msg) {
		var event domain.ScaleEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("[NATS-SUB] [ERROR] Failed to unmarshal scale event: %v", err)
			return
		}

		log.Printf("[NATS-SUB] [INFO] Received scale event: action=%s target=%s", event.Action, event.Policy.TargetID)

		go func() {
			if err := s.handler.EnforceScaling(context.Background(), &event); err != nil {
				log.Printf("[NATS-SUB] [ERROR] Scaling enforcement failed for %s: %v", event.Policy.TargetID, err)
			}
		}()
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", scaleSubject, err)
	}

	// 2. Subscribe to VM Provision Events
	provisionSubject := "dev.ec2.v1.vm.provision"
	_, err = s.nc.QueueSubscribe(provisionSubject, queueGroup, func(msg *nats.Msg) {
		var event domain.GameVMProvisionEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("[NATS-SUB] [ERROR] Failed to unmarshal provision event: %v", err)
			return
		}

		log.Printf("[NATS-SUB] [INFO] Received provision event for game: %s (ID: %d)", event.GameName, event.GameID)

		go func() {
			if err := s.handler.HandleVMProvision(context.Background(), &event); err != nil {
				log.Printf("[NATS-SUB] [ERROR] VM provision failed for game %d: %v", event.GameID, err)
			}
		}()
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", provisionSubject, err)
	}

	log.Printf("[NATS-SUB] Successfully subscribed to %s and %s", scaleSubject, provisionSubject)
	return nil
}

func (s *NATSSubscriber) Close() {
	if s.nc != nil {
		s.nc.Close()
	}
}

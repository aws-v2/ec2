package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/nats-io/nats.go"
)

// ScalingEnforcer is the interface the subscriber expects to call when a scale event occurs.
type ScalingEnforcer interface {
	EnforceScaling(ctx context.Context, event *domain.ScaleEvent) error
}

type NATSSubscriber struct {
	nc       *nats.Conn
	enforcer ScalingEnforcer
}

func NewNATSSubscriber(url, user, password string, enforcer ScalingEnforcer) (*NATSSubscriber, error) {
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
		nc:       nc,
		enforcer: enforcer,
	}, nil
}

func (s *NATSSubscriber) Start() error {
	subject := "dev.ec2.v1.scale.*"
	queueGroup := "ec2-enforcers"

	_, err := s.nc.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
		var event domain.ScaleEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("[NATS-SUB] [ERROR] Failed to unmarshal scale event: %v", err)
			return
		}

		log.Printf("[NATS-SUB] [INFO] Received scale event: action=%s target=%s", event.Action, event.Policy.TargetID)

		// Process the event in a background goroutine so we don't block the NATS message loop
		go func() {
			if err := s.enforcer.EnforceScaling(context.Background(), &event); err != nil {
				log.Printf("[NATS-SUB] [ERROR] Scaling enforcement failed for %s: %v", event.Policy.TargetID, err)
			} else {
				log.Printf("[NATS-SUB] [SUCCESS] Scaling enforcement successful for %s", event.Policy.TargetID)
			}
		}()
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}

	log.Printf("[NATS-SUB] Successfully subscribed to %s (queue: %s)", subject, queueGroup)
	return nil
}

func (s *NATSSubscriber) Close() {
	if s.nc != nil {
		s.nc.Close()
	}
}

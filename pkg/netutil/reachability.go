package netutil

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"
)

// CheckReachability performs a TCP reachability check on a host and port.
func CheckReachability(serviceName, host string, port int, attempts int, delay time.Duration) error {
	address := fmt.Sprintf("%s:%d", host, port)
	var lastErr error

	for i := 1; i <= attempts; i++ {
		slog.Info("Checking service reachability",
			"service", serviceName,
			"address", address,
			"attempt", i,
			"max_attempts", attempts,
		)

		conn, err := net.DialTimeout("tcp", address, delay)
		if err == nil {
			conn.Close()
			slog.Info("Service is reachable", "service", serviceName, "address", address)
			return nil
		}

		lastErr = err
		slog.Warn("Service not reachable yet",
			"service", serviceName,
			"address", address,
			"error", err,
			"retry_in", delay.String(),
		)
		time.Sleep(delay)
	}

	return fmt.Errorf("service %s at %s remains unreachable after %d attempts: %w", serviceName, address, attempts, lastErr)
}

// CheckReachabilityURL parses a NATS-style URL (nats://host:port) and checks reachability.
func CheckReachabilityURL(serviceName, rawURL string, attempts int, delay time.Duration) error {
	// Simple parsing for nats://host:port or host:port
	host, portStr, err := net.SplitHostPort(stripProtocol(rawURL))
	if err != nil {
		return fmt.Errorf("invalid address format for %s (%s): %w", serviceName, rawURL, err)
	}

	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if port == 0 {
		return fmt.Errorf("invalid port for %s: %s", serviceName, portStr)
	}

	return CheckReachability(serviceName, host, port, attempts, delay)
}

func stripProtocol(rawURL string) string {
	if idx := strings.LastIndexAny(rawURL, "/"); idx != -1 {
		return rawURL[idx+1:]
	}
	return rawURL
}

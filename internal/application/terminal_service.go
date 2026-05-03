package application

import (
	"context"
	domain "ec2-api/internal/domain/instance"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ─── Terminal Service ─────────────────────────────────────────────────────────

type TerminalService struct {
	instanceService *InstanceService
	agentURL        string

	mu          sync.Mutex
	connections map[string]*websocket.Conn
}

func NewTerminalService(instanceService *InstanceService, agentURL string) *TerminalService {
	return &TerminalService{
		instanceService: instanceService,
		agentURL:        agentURL,
		connections:     make(map[string]*websocket.Conn),
	}
}

// ─── Message Types ──────────────────────────────────────────────────────────────

type TerminalMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`

	// open_terminal
	VMIP      string `json:"vm_ip,omitempty"`
	VMSSHPort int    `json:"vm_ssh_port,omitempty"`
	SSHUser   string `json:"ssh_user,omitempty"`
	SSHKey    string `json:"ssh_key,omitempty"` // base64-encoded PEM

	// terminal_data
	Data string `json:"data,omitempty"`

	// resize
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// terminal_closed
	Reason string `json:"reason,omitempty"`
}

// ─── WebSocket Upgrader ─────────────────────────────────────────────────────────

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for now
	},
}

// ─── HTTP Handler ───────────────────────────────────────────────────────────────

func (s *TerminalService) HandleTerminal(w http.ResponseWriter, r *http.Request, c *gin.Context) {
	instanceID := r.URL.Query().Get("instance_id")
	if instanceID == "" {
		http.Error(w, "missing instance_id", http.StatusBadRequest)
		return
	}

	// Get instance details from EC2 service
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	instance, err := s.instanceService.GetInstance(instanceID, c.GetString("userID"))
	if err != nil {
		http.Error(w, fmt.Sprintf("instance not found: %v", err), http.StatusNotFound)
		return
	}

	// Build agent URL
	agentURL := s.agentURL
	if !strings.HasPrefix(agentURL, "ws") {
		agentURL = "ws://" + agentURL
	}
	agentURL += fmt.Sprintf("/ws/terminal?instance_id=%s", instanceID)

	// Upgrade WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("failed to upgrade WebSocket", "error", err)
		return
	}
	defer conn.Close()

	slog.Info("Terminal WebSocket connected", "instance_id", instanceID, "agent_url", agentURL)

	// Store connection
	s.mu.Lock()
	s.connections[instanceID] = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.connections, instanceID)
		s.mu.Unlock()
		slog.Info("Terminal WebSocket disconnected", "instance_id", instanceID)
	}()

	// Open terminal on agent
	if err := s.openTerminalOnAgent(ctx, instance, conn); err != nil {
		slog.Error("failed to open terminal on agent", "error", err)
		return
	}
}

// ─── Open Terminal on Agent ─────────────────────────────────────────────────────

func (s *TerminalService) openTerminalOnAgent(ctx context.Context, instance *domain.Instance, conn *websocket.Conn) error {
	// Get SSH key from instance (base64 encoded PEM)
	privateKey, err := base64.StdEncoding.DecodeString(instance.IP)
	if err != nil {
		return fmt.Errorf("failed to decode private key: %w", err)
	}

	// Build agent URL
	agentURL := s.agentURL
	if !strings.HasPrefix(agentURL, "ws") {
		agentURL = "ws://" + agentURL
	}
	agentURL += "/ws/terminal"

	// Connect to agent
	agentConn, _, err := websocket.DefaultDialer.DialContext(ctx, agentURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer agentConn.Close()

	// Send open_terminal message
	msg := TerminalMessage{
		Type:      "open_terminal",
		SessionID: instance.ID,
		VMIP:      instance.IP,
		VMSSHPort: 22, // Assuming default SSH port
		SSHUser:   "ubuntu", // Assuming default user
		SSHKey:    string(privateKey),
	}

	if err := agentConn.WriteJSON(msg); err != nil {
		return fmt.Errorf("failed to send open_terminal: %w", err)
	}

	// Goroutine: forward messages from agent to client
	go func() {
		for {
			var agentMsg TerminalMessage
			if err := agentConn.ReadJSON(&agentMsg); err != nil {
				// Agent disconnected
				closeMsg := TerminalMessage{
					Type:      "terminal_closed",
					SessionID: instance.ID,
					Reason:    "agent disconnected",
				}
				conn.WriteJSON(closeMsg)
				return
			}

			if err := conn.WriteJSON(agentMsg); err != nil {
				return
			}
		}
	}()

	// Goroutine: forward messages from client to agent
	for {
		var clientMsg TerminalMessage
		if err := conn.ReadJSON(&clientMsg); err != nil {
			// Client disconnected
			break
		}

		if err := agentConn.WriteJSON(clientMsg); err != nil {
			break
		}
	}

	// Close terminal on agent
	closeMsg := TerminalMessage{
		Type:      "terminal_closed",
		SessionID: instance.ID,
		Reason:    "client disconnected",
	}
	agentConn.WriteJSON(closeMsg)

	return nil
}

// ─── Helper Methods ─────────────────────────────────────────────────────────────

func (s *TerminalService) CloseTerminal(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, ok := s.connections[instanceID]
	if !ok {
		return
	}
	conn.Close()
}
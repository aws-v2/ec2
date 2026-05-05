package application

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"sync"

	"ec2-api/internal/interfaces"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ─── Agent wire protocol ──────────────────────────────────────────────────────
// Mirrors the Message struct in agent.go exactly so both sides stay in sync.

type agentMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`

	// open_terminal
	VMIP      string `json:"vm_ip,omitempty"`
	VMSSHPort int    `json:"vm_ssh_port,omitempty"`
	SSHUser   string `json:"ssh_user,omitempty"`
	SSHKey    string `json:"ssh_key,omitempty"` // private key PEM

	// terminal_data (both directions)
	Data string `json:"data,omitempty"`

	// resize
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// terminal_closed
	Reason string `json:"reason,omitempty"`
}

// ─── TerminalService ──────────────────────────────────────────────────────────

// TerminalService opens agent sessions for browser terminals.
type TerminalService struct {
	instances interfaces.InstanceRepository
}

// ─── AgentSession ─────────────────────────────────────────────────────────────

// AgentSession is the object the transport layer holds.
// Out streams raw terminal bytes coming from the agent.
// Write / WindowChange / Close drive the agent from the browser side.
type AgentSession struct {
	// Out is an io.Reader whose bytes are terminal_data frames forwarded by the
	// agent.  The handler drains this in a goroutine and writes to the browser
	// WebSocket.
	Out io.Reader

	conn      *websocket.Conn
	sessionID string
	pw        *io.PipeWriter // closed when the read-loop exits

	writeMu sync.Mutex // serialise concurrent WriteJSON calls
	once    sync.Once  // guard Close
}

// Write sends raw bytes to the VM's stdin via the agent.
func (s *AgentSession) Write(p []byte) (int, error) {
	msg := agentMessage{
		Type:      "terminal_data",
		SessionID: s.sessionID,
		Data:      string(p),
	}
	s.writeMu.Lock()
	err := s.conn.WriteJSON(msg)
	s.writeMu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("agent write: %w", err)
	}
	return len(p), nil
}

// WindowChange forwards a terminal resize event to the agent.
func (s *AgentSession) WindowChange(rows, cols int) {
	if rows <= 0 || cols <= 0 {
		return
	}
	msg := agentMessage{
		Type:      "resize",
		SessionID: s.sessionID,
		Rows:      rows,
		Cols:      cols,
	}
	s.writeMu.Lock()
	_ = s.conn.WriteJSON(msg)
	s.writeMu.Unlock()
}

// Close tears down the agent session and the underlying WebSocket connection.
// Safe to call more than once.
func (s *AgentSession) Close() {
	s.once.Do(func() {
		log.Printf("[terminal-service] closing session %s", s.sessionID)

		// Ask the agent to clean up its SSH session.
		s.writeMu.Lock()
		_ = s.conn.WriteJSON(agentMessage{
			Type:      "close_terminal",
			SessionID: s.sessionID,
		})
		s.writeMu.Unlock()

		// Closing the pipe unblocks any reader of Out.
		s.pw.Close()
		s.conn.Close()
	})
}

// readLoop runs in its own goroutine and pumps agent frames into the Out pipe.
func (s *AgentSession) readLoop() {
	defer s.pw.Close() // unblock Out reader when we exit for any reason

	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			// Normal closure or network error — either way the session is done.
			log.Printf("[terminal-service] agent disconnected (session=%s): %v", s.sessionID, err)
			return
		}

		var msg agentMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[terminal-service] malformed agent frame (session=%s): %v", s.sessionID, err)
			continue
		}

		switch msg.Type {

		case "terminal_data":
			// Forward raw bytes to the Out pipe; the handler writes them to the
			// browser WebSocket.
			if _, err := s.pw.Write([]byte(msg.Data)); err != nil {
				log.Printf("[terminal-service] pipe write error (session=%s): %v", s.sessionID, err)
				return
			}

		case "terminal_closed":
			log.Printf("[terminal-service] agent closed session=%s reason=%q", s.sessionID, msg.Reason)
			return

		default:
			log.Printf("[terminal-service] unexpected agent message type=%q (session=%s)", msg.Type, s.sessionID)
		}
	}
}

// NewTerminalService creates a TerminalService backed by the given repository.
func NewTerminalService(repo interfaces.InstanceRepository) *TerminalService {
	return &TerminalService{instances: repo}
}

// CreateAgentSession resolves the instance, dials the agent's WebSocket, sends
// an open_terminal command, and returns a live AgentSession.
//
// The caller must call session.Close() when the terminal is done.
func (svc *TerminalService) CreateAgentSession(instanceID, userID string) (*AgentSession, error) {
	info, err := svc.instances.GetInstanceInfo(instanceID, userID)
	if err != nil {
		return nil, fmt.Errorf("instance lookup %q: %w", instanceID, err)
	}


	// Build the WebSocket URL from whatever scheme the caller stored.
	agentWS:= info.AgentURL
	if err != nil {
		return nil, fmt.Errorf("agent url: %w", err)
	}

	log.Printf("[terminal-service] dialling agent at %s for instance %s", agentWS, instanceID)

	conn, _, err := websocket.DefaultDialer.Dial(agentWS, nil)
	if err != nil {
		return nil, fmt.Errorf("dial agent %s: %w", agentWS, err)
	}

	sessionID := uuid.NewString()

	// Tell the agent to open an SSH connection to the target VM.
	openMsg := agentMessage{
		Type:      "open_terminal",
		SessionID: sessionID,
		VMIP:      info.VMIP,
		VMSSHPort: info.VMSSHPort,
		
		SSHUser:   "ubuntu",
		SSHKey:    info.SSHKey, // private key PEM
	}
	log.Printf("The && ssh key used in the terminal service %s for instance %s", info.SSHUser, instanceID)

	if err := conn.WriteJSON(openMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send open_terminal: %w", err)
	}

	pr, pw := io.Pipe()

	sess := &AgentSession{
		Out:       pr,
		conn:      conn,
		sessionID: sessionID,
		pw:        pw,
	}

	// Start pumping agent output into the pipe.
	go sess.readLoop()

	log.Printf("[terminal-service] session %s opened for instance %s", sessionID, instanceID)
	return sess, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// toWebSocketURL rewrites http/https to ws/wss and appends the /terminal path.
func toWebSocketURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already correct
	default:
		return "", fmt.Errorf("unsupported scheme %q (want http/https/ws/wss)", u.Scheme)
	}
	u.Path = "/terminal"

	return u.String(), nil
}
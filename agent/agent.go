package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// ─── TEST INTERCEPT ───────────────────────────────────────────────────────────
// Toggle this to true to bypass whatever the control plane sends
// and force all open_terminal requests to hit your real test machine.
// Set back to false when you're ready to use real VMs.
const TEST_MODE = true

// ── Plug your values in here ──────────────────────────────────────────────────
const TEST_VM_IP = "10.239.188.253" // ← replace with: hostname -I | awk '{print $1}'
const TEST_VM_SSH_PORT = 22       // ← usually 22, change if your machine uses another port
const TEST_SSH_USER = "martin"    // ← replace with your Linux username on that machine

// Paste the FULL output of: cat ~/.ssh/serwin_test
// Keep the backtick string exactly as-is, including newlines
const TEST_SSH_KEY = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACAMKLNcnw96DfFhlK/nuHQjAZNnGGmOZcLD4B6bUCybwwAAAJjIDjzcyA48
3AAAAAtzc2gtZWQyNTUxOQAAACAMKLNcnw96DfFhlK/nuHQjAZNnGGmOZcLD4B6bUCybww
AAAEBUwkL/d2bDvhDNlqI10t40wEuVOfvJZpZkQQt9/Jf1WAwos1yfD3oN8WGUr+e4dCMB
k2cYaY5lwsPgHptQLJvDAAAAEXNlcndpbi1hZ2VudC10ZXN0AQIDBA==
-----END OPENSSH PRIVATE KEY-----`

// ─────────────────────────────────────────────────────────────────────────────

// ─── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	NodeID     string
	ListenAddr string
}

func configFromEnv() Config {
	return Config{
		NodeID:     getEnv("NODE_ID", "node-1"),
		ListenAddr: getEnv("LISTEN_ADDR", ":9030"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Protocol ─────────────────────────────────────────────────────────────────

type Message struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`

	// open_terminal — all SSH details provided by control plane
	VMIP      string `json:"vm_ip,omitempty"`
	VMSSHPort int    `json:"vm_ssh_port,omitempty"`
	SSHUser   string `json:"ssh_user,omitempty"`
	SSHKey    string `json:"ssh_key,omitempty"` // private key PEM content

	// terminal_data (both directions)
	Data string `json:"data,omitempty"`

	// resize
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// terminal_closed
	Reason string `json:"reason,omitempty"`
}

// ─── SSH Session ──────────────────────────────────────────────────────────────

type Session struct {
	sshConn *ssh.Client
	sshSess *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader // grabbed before Shell() — must be stored here
	stderr  io.Reader // grabbed before Shell() — must be stored here
}

func (s *Session) Close() {
	if s.sshSess != nil {
		s.sshSess.Close()
	}
	if s.sshConn != nil {
		s.sshConn.Close()
	}
}

// ─── Agent ────────────────────────────────────────────────────────────────────

type Agent struct {
	cfg Config
}

func NewAgent(cfg Config) *Agent {
	return &Agent{cfg: cfg}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (a *Agent) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[agent] upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("[agent] control plane connected from %s", r.RemoteAddr)

	var (
		mu       sync.Mutex
		sessions = make(map[string]*Session)
		sendCh   = make(chan Message, 64)
	)

	send := func(msg Message) { sendCh <- msg }

	closeSession := func(sessionID, reason string) {
		mu.Lock()
		sess, ok := sessions[sessionID]
		if ok {
			delete(sessions, sessionID)
		}
		mu.Unlock()
		if ok {
			sess.Close()
			log.Printf("[agent] session %s closed: %s", sessionID, reason)
			send(Message{Type: "terminal_closed", SessionID: sessionID, Reason: reason})
		}
	}

	// Write loop
	go func() {
		for msg := range sendCh {
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("[agent] write error: %v", err)
				return
			}
		}
	}()

	// Read loop
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[agent] control plane disconnected: %v", err)
			break
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[agent] bad message: %v", err)
			continue
		}

		switch msg.Type {

		case "open_terminal":
			// ── TEST INTERCEPT ────────────────────────────────────────────────
			// When TEST_MODE is true, ignore whatever the control plane sent
			// and replace with your real machine's SSH details.
			if TEST_MODE {
				log.Printf("[agent] TEST_MODE: overriding VM details for session=%s", TEST_SSH_KEY)
				log.Printf("[agent] TEST_MODE: original target was vm=%s user=%s", msg.VMIP, msg.SSHUser)
				msg.VMIP = TEST_VM_IP
				msg.VMSSHPort = TEST_VM_SSH_PORT
				msg.SSHUser = TEST_SSH_USER
				msg.SSHKey = TEST_SSH_KEY
			}
			// ─────────────────────────────────────────────────────────────────

			log.Printf("[agent] open_terminal session=%s vm=%s user=%s", msg.SessionID, msg.VMIP, msg.SSHUser)
			go func(msg Message) {
				sess, err := dialSSH(msg.VMIP, msg.VMSSHPort, msg.SSHUser, msg.SSHKey)
				if err != nil {
					log.Printf("[agent] ssh failed: %v", err)
					send(Message{Type: "terminal_closed", SessionID: msg.SessionID, Reason: err.Error()})
					return
				}

				// Pipes are already stored on sess — grabbed before Shell() in dialSSH.
				// Do NOT call StdoutPipe/StderrPipe again here; they return nil after Shell starts.
				mu.Lock()
				sessions[msg.SessionID] = sess
				mu.Unlock()

				// Forward SSH output → control plane
				go func() {
					buf := make([]byte, 4096)
					combined := io.MultiReader(sess.stdout, sess.stderr)
					for {
						n, err := combined.Read(buf)
						if n > 0 {
							send(Message{Type: "terminal_data", SessionID: msg.SessionID, Data: string(buf[:n])})
						}
						if err != nil {
							break
						}
					}
					closeSession(msg.SessionID, "ssh output closed")
				}()
			}(msg)

		case "terminal_data":
			mu.Lock()
			sess, ok := sessions[msg.SessionID]
			mu.Unlock()
			if ok {
				sess.stdin.Write([]byte(msg.Data))
			}

		case "resize":
			mu.Lock()
			sess, ok := sessions[msg.SessionID]
			mu.Unlock()
			if ok && msg.Cols > 0 && msg.Rows > 0 {
				sess.sshSess.WindowChange(msg.Rows, msg.Cols)
			}

		case "close_terminal":
			closeSession(msg.SessionID, "requested by control plane")

		default:
			log.Printf("[agent] unknown message type: %s", msg.Type)
		}
	}

	// Clean up all sessions when control plane disconnects
	mu.Lock()
	for id, sess := range sessions {
		sess.Close()
		log.Printf("[agent] cleaned up session %s", id)
	}
	mu.Unlock()
}

// dialSSH opens an SSH connection using credentials provided by the control plane.
func dialSSH(vmIP string, vmPort int, sshUser, sshKeyPEM string) (*Session, error) {
	if vmPort == 0 {
		vmPort = 22
	}

	signer, err := ssh.ParsePrivateKey([]byte(sshKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(vmIP, fmt.Sprintf("%d", vmPort))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	sshSess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("new session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSess.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		sshSess.Close()
		client.Close()
		return nil, fmt.Errorf("pty: %w", err)
	}

	// All three pipes MUST be obtained before Shell() is called.
	// After Shell() starts, StdoutPipe/StderrPipe return nil → panic.
	stdin, err := sshSess.StdinPipe()
	if err != nil {
		sshSess.Close()
		client.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := sshSess.StdoutPipe()
	if err != nil {
		sshSess.Close()
		client.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := sshSess.StderrPipe()
	if err != nil {
		sshSess.Close()
		client.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := sshSess.Shell(); err != nil {
		sshSess.Close()
		client.Close()
		return nil, fmt.Errorf("shell: %w", err)
	}

	log.Printf("[agent] ssh connected → %s@%s", sshUser, addr)
	return &Session{sshConn: client, sshSess: sshSess, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg := configFromEnv()

	// Print TEST_MODE status on startup so it's obvious
	if TEST_MODE {
		log.Printf("[agent] ⚠️  TEST_MODE=true — all SSH sessions → %s@%s:%d", TEST_SSH_USER, TEST_VM_IP, TEST_VM_SSH_PORT)
	}

	agent := NewAgent(cfg)

	http.HandleFunc("/terminal", agent.HandleConnection)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"node_id": cfg.NodeID,
		})
		log.Printf("[agent] health check from %s", r.RemoteAddr)
	})

	log.Printf("[agent] node=%s listening on %s", cfg.NodeID, cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, nil))
}

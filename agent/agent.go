package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// ─── CONFIG ────────────────────────────────────────────────────────────────

const TEST_MODE = true

var (
	NodeID          = "default-node"
	ControlPlaneURL = "ws://localhost:8080/agent"
)

// ─── CONFIG STRUCT ──────────────────────────────────────────────────────────

type Config struct {
	NodeID     string
	ListenAddr string
}

func configFromEnv() Config {
	return Config{
		NodeID:     getEnv("NODE_ID", NodeID),
		ListenAddr: getEnv("LISTEN_ADDR", ":9030"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── MESSAGE PROTOCOL ──────────────────────────────────────────────────────

type Message struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`

	VMIP      string `json:"vm_ip,omitempty"`
	VMSSHPort int    `json:"vm_ssh_port,omitempty"`
	SSHUser   string `json:"ssh_user,omitempty"`
	SSHKey    string `json:"ssh_key,omitempty"`

	Data   string `json:"data,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ─── SESSION ───────────────────────────────────────────────────────────────

type Session struct {
	sshConn *ssh.Client
	sshSess *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

func (s *Session) Close() {
	if s.sshSess != nil {
		s.sshSess.Close()
	}
	if s.sshConn != nil {
		s.sshConn.Close()
	}
}

// ─── UTIL: GET HOST IP ─────────────────────────────────────────────────────

func getHostIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// ─── UTIL: READ SSH KEY FROM FILE ──────────────────────────────────────────

func readSSHKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	keyPath := filepath.Join(home, ".ssh", "serwin_test")

	data, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// ─── AGENT ──────────────────────────────────────────────────────────────────

type Agent struct {
	cfg Config
}

func NewAgent(cfg Config) *Agent {
	return &Agent{cfg: cfg}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ─── HANDLER ───────────────────────────────────────────────────────────────

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

	send := func(msg Message) {
		select {
		case sendCh <- msg:
		default:
			log.Printf("[agent] sendCh full, dropping message type=%s session=%s", msg.Type, msg.SessionID)
		}
	}

	// write loop — exits when sendCh is closed
	go func() {
		for msg := range sendCh {
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("[agent] write error: %v", err)
				return
			}
		}
	}()

	// close sendCh when the read loop exits so the write goroutine cleans up
	defer close(sendCh)

	closeSession := func(id, reason string) {
		mu.Lock()
		sess, ok := sessions[id]
		if ok {
			delete(sessions, id)
		}
		mu.Unlock()

		if ok {
			sess.Close()
			send(Message{Type: "terminal_closed", SessionID: id, Reason: reason})
			log.Printf("[agent] session %s closed: %s", id, reason)
		}
	}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[agent] disconnected: %v", err)
			break
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[agent] bad message: %v", err)
			continue
		}

		switch msg.Type {

		case "open_terminal":

			if TEST_MODE {
				hostIP, err := getHostIP()
				if err != nil {
					log.Printf("[agent] host IP error: %v", err)
					continue
				}

				key, err := readSSHKey()
				if err != nil {
					log.Printf("[agent] ssh key read error: %v", err)
					continue
				}

				msg.VMIP = hostIP
				msg.VMSSHPort = 22
				msg.SSHUser = getEnv("TEST_SSH_USER", "martin")
				msg.SSHKey = key
			}

			go func(msg Message) {
				log.Printf("[agent] opening terminal session=%s → %s@%s:%d cols=%d rows=%d",
					msg.SessionID, msg.SSHUser, msg.VMIP, msg.VMSSHPort, msg.Cols, msg.Rows)

				sess, err := dialSSH(msg.VMIP, msg.VMSSHPort, msg.SSHUser, msg.SSHKey, msg.Cols, msg.Rows)
				if err != nil {
					log.Printf("[agent] ssh dial failed session=%s: %v", msg.SessionID, err)
					send(Message{Type: "terminal_closed", SessionID: msg.SessionID, Reason: err.Error()})
					return
				}

				mu.Lock()
				sessions[msg.SessionID] = sess
				mu.Unlock()

				log.Printf("[agent] session %s established", msg.SessionID)

				// pipe a single reader into the send channel
				pipe := func(r io.Reader) {
					buf := make([]byte, 4096)
					for {
						n, err := r.Read(buf)
						if n > 0 {
							send(Message{
								Type:      "terminal_data",
								SessionID: msg.SessionID,
								Data:      string(buf[:n]),
							})
						}
						if err != nil {
							break
						}
					}
				}

				// stdout and stderr pumped concurrently
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); pipe(sess.stdout) }()
				go func() { defer wg.Done(); pipe(sess.stderr) }()

				wg.Wait()
				closeSession(msg.SessionID, "ssh closed")
			}(msg)

		case "terminal_data":
			mu.Lock()
			sess := sessions[msg.SessionID]
			mu.Unlock()

			if sess != nil {
				if _, err := sess.stdin.Write([]byte(msg.Data)); err != nil {
					log.Printf("[agent] stdin write error session=%s: %v", msg.SessionID, err)
				}
			}

		case "resize":
			mu.Lock()
			sess := sessions[msg.SessionID]
			mu.Unlock()

			if sess != nil && msg.Rows > 0 && msg.Cols > 0 {
				if err := sess.sshSess.WindowChange(msg.Rows, msg.Cols); err != nil {
					log.Printf("[agent] resize error session=%s: %v", msg.SessionID, err)
				}
			}

		case "close_terminal":
			closeSession(msg.SessionID, "client closed")

		default:
			log.Printf("[agent] unknown message type: %s", msg.Type)
		}
	}

	// clean up any sessions still open when the control plane disconnects
	mu.Lock()
	remaining := make(map[string]*Session, len(sessions))
	for id, sess := range sessions {
		remaining[id] = sess
	}
	mu.Unlock()

	for id, sess := range remaining {
		log.Printf("[agent] force-closing session %s on disconnect", id)
		sess.Close()
		delete(sessions, id)
	}
}

// ─── SSH DIAL ───────────────────────────────────────────────────────────────

func dialSSH(vmIP string, vmPort int, user, keyPEM string, cols, rows int) (*Session, error) {
	if vmPort == 0 {
		vmPort = 22
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	signer, err := ssh.ParsePrivateKey([]byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(vmIP, fmt.Sprintf("%d", vmPort))

	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("new session: %w", err)
	}

	// ── request PTY before Shell() so bash stays alive ──────────────────────
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := sess.StderrPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("shell: %w", err)
	}

	return &Session{
		sshConn: client,
		sshSess: sess,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
	}, nil
}

// ─── MAIN ───────────────────────────────────────────────────────────────────

func main() {
	cfg := configFromEnv()

	if TEST_MODE {
		ip, _ := getHostIP()
		log.Printf("[agent] TEST MODE → %s@%s:22", getEnv("TEST_SSH_USER", "martin"), ip)
	}

	agent := NewAgent(cfg)
	http.HandleFunc("/terminal", agent.HandleConnection)

	log.Printf("[agent] listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, nil))
}
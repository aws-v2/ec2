package domain

import (
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

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




type Config struct {
	NodeID          string
	ListenAddr      string
	ControlPlaneURL string
	APIKey          string
}





var (
	DefaultNodeID          = "default-node"
	DefaultControlPlaneURL = "http://localhost:8080"
)




// ─── GAME STRUCTS ────────────────────────────────────────────────────────────

type GameMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id"`
	Data      json.RawMessage `json:"data,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	// open_game fields
	VMIP   string `json:"vm_ip,omitempty"`
	VMPort int    `json:"vm_port,omitempty"`
}

type GameSession struct {
	id   string
	conn net.Conn // TCP connection to Godot process in the VM
	once sync.Once
}


type Agent struct {
	cfg Config
}
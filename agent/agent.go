package main

import (
	"bufio"
	"bytes"
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


// ─── CONFIG STRUCT ──────────────────────────────────────────────────────────



func (s *Session) Close() {
	if s.sshSess != nil {
		s.sshSess.Close()
	}
	if s.sshConn != nil {
		s.sshConn.Close()
	}
}


// ─── AGENT ──────────────────────────────────────────────────────────────────



func NewAgent(cfg Config) *Agent {
	return &Agent{cfg: cfg}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}





func newGameSession(id string, conn net.Conn) *GameSession {
	return &GameSession{id: id, conn: conn}
}

func (s *GameSession) Close() {
	s.once.Do(func() { s.conn.Close() })
}

// WriteToGodot sends newline-delimited JSON to the Godot TCP socket
func (s *GameSession) WriteToGodot(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	_, err := s.conn.Write(data)
	return err
}




// ─── MAIN ───────────────────────────────────────────────────────────────────

func main() {
	cfg := configFromEnv()

	if TEST_MODE {
		ip, _ := getHostIP()
		log.Printf("[agent] TEST MODE → %s@%s:22", getEnv("TEST_SSH_USER", "martin"), ip)
	}

	agent := NewAgent(cfg)
	
	log.Printf("[agent] starting with NodeID=%s, ControlPlane=%s, ListenAddr=%s", 
		cfg.NodeID, cfg.ControlPlaneURL, cfg.ListenAddr)

	// Start metrics reporting in background
	go agent.reportMetrics()

	http.HandleFunc("/terminal", agent.HandleConnection)
	http.HandleFunc("/game", agent.HandleGameConnection)   // ← new

	log.Printf("[agent] listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, nil))
}
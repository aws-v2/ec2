package handlers

import (
	"ec2-api/agent/domain"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)





// ─── GAME HANDLER ────────────────────────────────────────────────────────────
// Flow:
//   open_game  → agent dials Godot TCP on VM, starts relay
//   game_input → agent writes keyboard/mouse JSON to Godot
//   game_state → Godot output relayed back to frontend (wrapped in GameMessage)
//   close_game → agent closes session cleanly

func (a *Agent) HandleGameConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[agent] game upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[agent] game frontend connected from %s", r.RemoteAddr)

	var (
		mu       sync.Mutex
		sessions = make(map[string]*GameSession)
		sendCh   = make(chan GameMessage, 128)
	)

	send := func(msg GameMessage) {
		select {
		case sendCh <- msg:
		default:
			log.Printf("[agent] game sendCh full, dropping type=%s session=%s", msg.Type, msg.SessionID)
		}
	}

	// write loop — one goroutine owns the WS writer
	go func() {
		for msg := range sendCh {
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("[agent] game write error: %v", err)
				return
			}
		}
	}()
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
			send(GameMessage{Type: "game_closed", SessionID: id, Reason: reason})
			log.Printf("[agent] game session %s closed: %s", id, reason)
		}
	}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[agent] game frontend disconnected: %v", err)
			break
		}

		var msg GameMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[agent] game bad message: %v", err)
			continue
		}

		switch msg.Type {

		case "open_game":
			go func(msg GameMessage) {
				addr := fmt.Sprintf("%s:%d", msg.VMIP, msg.VMPort)
				log.Printf("[agent] dialing Godot at %s session=%s", addr, msg.SessionID)

				tcpConn, err := net.DialTimeout("tcp", addr, 10*time.Second)
				if err != nil {
					log.Printf("[agent] godot dial failed session=%s: %v", msg.SessionID, err)
					send(GameMessage{Type: "game_closed", SessionID: msg.SessionID, Reason: err.Error()})
					return
				}

				sess := newGameSession(msg.SessionID, tcpConn)

				mu.Lock()
				sessions[msg.SessionID] = sess
				mu.Unlock()

				log.Printf("[agent] game session %s ready → %s", msg.SessionID, addr)
				send(GameMessage{Type: "game_ready", SessionID: msg.SessionID})

				// Godot → frontend relay
				// Godot sends newline-delimited JSON lines, we wrap each in a GameMessage envelope
				scanner := bufio.NewScanner(tcpConn)
				for scanner.Scan() {
					line := make([]byte, len(scanner.Bytes()))
					copy(line, scanner.Bytes())
					send(GameMessage{
						Type:      "game_state",
						SessionID: msg.SessionID,
						Data:      json.RawMessage(line),
					})
				}

				closeSession(msg.SessionID, "godot socket closed")
			}(msg)

		case "game_input":
			// frontend keyboard/mouse → Godot
			mu.Lock()
			sess := sessions[msg.SessionID]
			mu.Unlock()

			if sess == nil {
				log.Printf("[agent] game_input for unknown session=%s", msg.SessionID)
				continue
			}
			if err := sess.WriteToGodot(msg.Data); err != nil {
				log.Printf("[agent] godot write error session=%s: %v", msg.SessionID, err)
				closeSession(msg.SessionID, "godot write error")
			}

		case "close_game":
			closeSession(msg.SessionID, "client closed")

		default:
			log.Printf("[agent] unknown game message type: %s", msg.Type)
		}
	}

	// force-close all sessions when frontend disconnects
	mu.Lock()
	remaining := make(map[string]*GameSession, len(sessions))
	for id, sess := range sessions {
		remaining[id] = sess
	}
	mu.Unlock()

	for id, sess := range remaining {
		log.Printf("[agent] force-closing game session %s on frontend disconnect", id)
		sess.Close()
		delete(sessions, id)
	}
}

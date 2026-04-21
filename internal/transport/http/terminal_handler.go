package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"ec2-api/internal/application"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // In production, check origins
	},
}

type TerminalHandler struct {
	service *application.TerminalService
}

func NewTerminalHandler(service *application.TerminalService) *TerminalHandler {
	return &TerminalHandler{service: service}
}

type TerminalMessage struct {
	Type string `json:"type"` // "stdin", "resize"
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (h *TerminalHandler) HandleTerminal(c *gin.Context) {
	instanceID := c.Param("id")
	log.Printf("[TERMINAL] Handling request for instance: %s", instanceID)

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("failed to upgrade to websocket: %v", err)
		return
	}
	defer ws.Close()

	userID := c.GetString("userID")
	sshSession, err := h.service.CreateSSHSession(instanceID, userID)
	if err != nil {
		log.Printf("[TERMINAL] SSH connection failed.: %v", err)
		errorMessage := fmt.Sprintf("\r\n[ERROR] Failed to connect: %v\r\n", err)
		ws.WriteJSON(TerminalMessage{
			Type: "error",
			Data: errorMessage,
		})
		return
	}
	defer sshSession.Close()

	log.Printf("[TERMINAL] SSH session established for %s, starting bridge", instanceID)

	// SSH -> WebSocket
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := sshSession.Out.Read(buf)
			if err != nil {
				log.Printf("[TERMINAL] SSH output closed: %v", err)
				return
			}
			msg := TerminalMessage{
				Type: "stdout",
				Data: string(buf[:n]),
			}
			if err := ws.WriteJSON(msg); err != nil {
				log.Printf("[TERMINAL] WS write failed: %v", err)
				return
			}
		}
	}()

	// WebSocket -> SSH
	for {
		_, p, err := ws.ReadMessage()
		if err != nil {
			return
		}

		var msg TerminalMessage
		if err := json.Unmarshal(p, &msg); err != nil {
			// If not JSON, maybe raw data (fallback)
			if _, err := sshSession.In.Write(p); err != nil {
				return
			}
			continue
		}

		switch msg.Type {
		case "stdin":
			if _, err := sshSession.In.Write([]byte(msg.Data)); err != nil {
				return
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				sshSession.Session.WindowChange(msg.Rows, msg.Cols)
			}
		}
	}
}

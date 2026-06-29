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
		return true
	},
}

type TerminalHandler struct {
	service *application.TerminalService
}

func NewTerminalHandler(service *application.TerminalService) *TerminalHandler {
	return &TerminalHandler{service: service}
}

type TerminalMessage struct {
	Type string `json:"type"` // "stdin", "resize", "stdout", "error"
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (h *TerminalHandler) HandleTerminal(c *gin.Context) {
	instanceID := c.Param("id")
	sessionID := c.Param("session")
	userID := c.GetString("userID")
	token := c.GetString("token")

	log.Printf("[TERMINAL] Handling request for instance: %s", instanceID)
	log.Printf("[TERMINAL] Handling request for instance:-- %s", token)

	// Resolve the agent session BEFORE upgrading to WebSocket so we can
	// still return a proper HTTP error if the agent is unreachable / nil.
	session, err := h.service.CreateAgentSession(instanceID, userID,sessionID, instanceID,token)
	if err != nil {
		log.Printf("[TERMINAL] Agent connection failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": fmt.Sprintf("failed to connect to agent: %v", err),
		})
		return
	}

	// Only upgrade once we know the agent is reachable.
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		session.Close()
		log.Printf("[TERMINAL] Failed to upgrade to websocket: %v", err)
		return
	}
	defer ws.Close()
	defer session.Close()

	log.Printf("[TERMINAL] Agent session established for %s, starting bridge", instanceID)

	// Agent -> WebSocket
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := session.Out.Read(buf)
			if err != nil {
				log.Printf("[TERMINAL] Agent output closed: %v", err)
				ws.WriteJSON(TerminalMessage{
					Type: "error",
					Data: "\r\n[INFO] Terminal session closed.\r\n",
				})
				return
			}
			if err := ws.WriteJSON(TerminalMessage{
				Type: "stdout",
				Data: string(buf[:n]),
			}); err != nil {
				log.Printf("[TERMINAL] WS write failed: %v", err)
				return
			}
		}
	}()

	// WebSocket -> Agent
	for {
		_, p, err := ws.ReadMessage()
		if err != nil {
			log.Printf("[TERMINAL] WS read closed: %v", err)
			return
		}

		var msg TerminalMessage
		if err := json.Unmarshal(p, &msg); err != nil {
			// Not JSON — pass raw bytes through directly.
			if _, err := session.Write(p); err != nil {
				log.Printf("[TERMINAL] Agent write failed: %v", err)
				return
			}
			continue
		}

		switch msg.Type {
		case "stdin":
			if _, err := session.Write([]byte(msg.Data)); err != nil {
				log.Printf("[TERMINAL] Agent stdin write failed: %v", err)
				return
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				session.WindowChange(msg.Rows, msg.Cols)
			}
		}
	}
}
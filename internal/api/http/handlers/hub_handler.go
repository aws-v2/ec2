package transport

import (
	"ec2-api/internal/infra/notifications"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

type NotificationHandler struct {
	hub *notifications.Hub
}

func NewNotificationHandler(hub *notifications.Hub) *NotificationHandler {
	return &NotificationHandler{hub: hub}
}

// Stream is the SSE endpoint: GET /api/v1/ec2/notifications/stream
func (h *NotificationHandler) Stream(c *gin.Context) {
	userID := c.GetString("userID") // must match the key your AuthMiddleware sets

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch, snapshot, unsub := h.hub.Subscribe(userID)
	defer unsub()

	send := func(b []byte) {
		fmt.Fprintf(c.Writer, "event: notification\ndata: %s\n\n", b)
		c.Writer.Flush()
	}

	for _, n := range snapshot {
		b, _ := json.Marshal(n)
		send(b)
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			send(b)
		case <-heartbeat.C:
			fmt.Fprint(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		}
	}
}
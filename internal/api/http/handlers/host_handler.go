package transport

import (
	"ec2-api/internal/application"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/host"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type HostHandler struct {
	service *application.HostService
}

func NewHostHandler(service *application.HostService) *HostHandler {
	return &HostHandler{service: service}
}


func (h *HostHandler) AddTemplate(c *gin.Context) {
	userID := c.GetString("user_id")

	correlationID := c.GetHeader("X-Correlation-ID")
	if correlationID == "" {
		correlationID = uuid.New().String()
	}

	var req struct {
		TemplateARN string `json:"template_arn"`
		Extension   string `json:"extension"` // optional (.zip default)
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	key, err := h.service.AddTemplate(
		c.Request.Context(),
		userID,
		correlationID,
		req.TemplateARN,
		req.Extension,
	)

	if err != nil {
		log.Printf("[templates] add failed: %v", err)
		c.JSON(500, gin.H{"error": "failed to add template"})
		return
	}

	c.JSON(200, gin.H{
		"template_key": key,
		"bucket":       "templatebucket-default",
	})
}

func (h *HostHandler) GetHostTemplates(c *gin.Context) {
	userID := c.GetString("user_id")

	correlationID := c.GetHeader("X-Correlation-ID")
	if correlationID == "" {
		correlationID = uuid.New().String()
	}

	url, err := h.service.GetHostTemplates(
		c.Request.Context(),
		userID,
		correlationID,
	)

	if err != nil {
		log.Printf("[templates] failed: %v", err)
		c.JSON(500, gin.H{"error": "failed to fetch templates"})
		return
	}

	c.JSON(200, gin.H{
		"download_url": url,
	})
}
func (h *HostHandler) HandleHeartbeat(c *gin.Context) {
    var req domain.HeartbeatRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        dto.SendError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
        return
    }

    resp, err := h.service.HandleHeartbeat(req)
    if err != nil {
        dto.SendError(c, http.StatusInternalServerError, err.Error())
        return
    }

    dto.SendSuccess(c, http.StatusOK, "Heartbeat recorded", resp)
}
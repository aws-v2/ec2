package transport

import (
	"ec2-api/internal/application"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/host"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type HostHandler struct {
	service *application.HostService
}

func NewHostHandler(service *application.HostService) *HostHandler {
	return &HostHandler{service: service}
}

//    run: |
//           curl -X POST \
//             "http://${STAGING_CONTROL_PLANE}/api/v1/compute/host/update-agent" \
//             -H "X-Api-Key: ${API_KEY}" \
//             -H "Content-Type: application/json" \
//             -d '{
//               "bucket": "agent-binary-system",
//               "file_name": "agent-${{ steps.version.outputs.VERSION }}"
//             }'



func (h *HostHandler) UpdateAgent(c *gin.Context) {
	var req domain.RolloutUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}


	req.UserID = c.GetString("userID")
	
	
	
	// req.Bucket and req.FileName come from the JSON body
	// req.Version is derived from the file name eg agent-1.0.2 → 1.0.2
	if req.FileName != "" && req.Version == "" {
		parts := strings.SplitN(req.FileName, "-", 2)
		if len(parts) == 2 {
			req.Version = parts[1]
		}
	}




	summary, err := h.service.RolloutAgentUpdate(c.Request.Context(),req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, summary)
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
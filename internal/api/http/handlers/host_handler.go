package transport

import (
	"ec2-api/internal/application"
	domain "ec2-api/internal/domain/host"
	"ec2-api/internal/vpcpkg"
	"fmt"
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

type RefreshRequest struct {
	HostType string `json:"host-type"`
	HostID   string `json:"host-id"`
	Env      string `json:"env"`
}
type RefreshIp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (h *HostHandler) CPHost(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:CPHost] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}
	var data string

	switch req.Env {
	case "dev":
		data = "http://localhost:8080"
	case "staging":
		data = "http://localhost:8080"
	case "prod":
		data = "http://102.10.98.23:8080"
	default:
		data = "http://localhost:8080"

	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Refresh request executed successfully", gin.H{"data": data})

}

func (h *HostHandler) DownloadTemplate(c *gin.Context) {

	var req domain.DowloadTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:DownloadTemplate] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	userID := c.GetString("userID")
	log.Printf("--->: %v", req.HostID)
	log.Printf("--->: %v", req.ImageType)

	url, err := h.service.DownloadTemplate(c.Request.Context(), req, userID)
	if err != nil {
		log.Printf("[Handler:DownloadTemplate] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to fetch download url"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Download URL fetched", domain.DowloadTemplateResp{ImageUrl: url})

}
func (h *HostHandler) UpdateAgent(c *gin.Context) {
	var req domain.RolloutUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:UpdateAgent] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	log.Printf("The update agentrequest is as follows, %v", req)

	req.UserID = c.GetString("userID")

	// req.Bucket and req.FileName come from the JSON body
	// req.Version is derived from the file name eg agent-1.0.2 → 1.0.2
	if req.FileName != "" && req.Version == "" {
		parts := strings.SplitN(req.FileName, "-", 2)
		if len(parts) == 2 {
			req.Version = parts[1]
		}
	}

	summary, err := h.service.RolloutAgentUpdate(c.Request.Context(), req)
	if err != nil {
		log.Printf("[Handler:UpdateAgent] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to start rollout"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Rollout started", summary)
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
		log.Printf("[Handler:AddTemplate] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
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
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to add template"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Template created", gin.H{"template_key": key, "bucket": "templatebucket-default"})
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
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to fetch templates"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Download URL fetched", gin.H{"download_url": url})
}
func (h *HostHandler) HandleHeartbeat(c *gin.Context) {
	var req domain.HeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Log the specific unmarshaling error to the console
		log.Printf("[host-handler] Heartbeat bind error: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	resp, err := h.service.HandleHeartbeat(c,req)
	if err != nil {
		log.Printf("[host-handler] Heartbeat service error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to record heartbeat"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Heartbeat recorded", resp)
}

func (h *HostHandler) HandleHealth(c *gin.Context) {
	vpcpkg.RespondSucces(c, http.StatusOK, "Heartbeat recorded", gin.H{"status":"ok"})

}



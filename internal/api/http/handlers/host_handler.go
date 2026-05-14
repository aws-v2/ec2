package transport

import (
	"ec2-api/internal/application"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/host"
	"net/http"

	"github.com/gin-gonic/gin"
)

type HostHandler struct {
	service *application.HostService
}

func NewHostHandler(service *application.HostService) *HostHandler {
	return &HostHandler{service: service}
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
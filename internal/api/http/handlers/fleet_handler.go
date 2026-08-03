package transport

import (
	"fmt"
	"log"
	"net/http"

	"ec2-api/internal/application"
	"ec2-api/internal/vpcpkg"

	"github.com/gin-gonic/gin"
)

type FleetHandler struct {
	service *application.FleetService
}

func NewFleetHandler(service *application.FleetService) *FleetHandler {
	return &FleetHandler{service: service}
}

func (h *FleetHandler) GetOverview(c *gin.Context) {
	userID := c.GetString("userID")
	requestID := c.GetString("requestID")

	overview, err := h.service.GetOverview(userID)
	if err != nil {
		log.Printf("[Handler:GetManifest] Service call, requestID %s  error %s", requestID, err.Error())
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get overview"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Database provisioned successfully", overview)

}

func (h *FleetHandler) GetEvents(c *gin.Context) {
	userID := c.GetString("userID")
	requestID := c.GetString("requestID")

	events, err := h.service.GetEvents(userID)
	if err != nil {
		log.Printf("[Handler:GetManifest] Service call, requestID %s  error %s", requestID, err.Error())
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get overview"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   events,
	})
}

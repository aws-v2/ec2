package transport

import (
	"net/http"

	"github.com/Qarani-m/ec2-api/internal/application"
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
	if userID == "" {
		tokenString := GetTokenFromRequest(c)
		if tokenString != "" {
			var err error
			userID, err = ExtractUserIDFromToken(tokenString)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "unauthorized: " + err.Error()})
				return
			}
		}
	}

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "unauthorized"})
		return
	}

	overview, err := h.service.GetOverview(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   overview,
	})
}

func (h *FleetHandler) GetEvents(c *gin.Context) {
	userID := c.GetString("userID")
	if userID == "" {
		tokenString := GetTokenFromRequest(c)
		if tokenString != "" {
			var err error
			userID, err = ExtractUserIDFromToken(tokenString)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "unauthorized: " + err.Error()})
				return
			}
		}
	}

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "unauthorized"})
		return
	}

	events, err := h.service.GetEvents(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   events,
	})
}

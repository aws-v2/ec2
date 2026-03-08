package transport

import (
	"fmt"
	"net/http"

	"github.com/Qarani-m/ec2-api/internal/application"
	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/gin-gonic/gin"
)

type InstanceHandler struct {
	service *application.InstanceService
}

func NewInstanceHandler(service *application.InstanceService) *InstanceHandler {
	return &InstanceHandler{service: service}
}

func (h *InstanceHandler) RestartInstance(c *gin.Context) {
	instanceID := c.Param("id")
	userID := c.GetString("userID")

	err := h.service.RestartInstance(instanceID, userID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, fmt.Sprintf("Instance %s has been restarted", instanceID), nil)
}

func (h *InstanceHandler) CreateInstance(c *gin.Context) {
	var req domain.CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}
	userID := c.GetString("userID")
	instance, err := h.service.CreateInstance(&req, userID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	SendSuccess(c, http.StatusAccepted, "Instance creation started", instance)
}

func (h *InstanceHandler) GetInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	instance, err := h.service.GetInstance(id, userID)
	if err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Instance retrieved successfully", instance)
}

func (h *InstanceHandler) ListInstances(c *gin.Context) {
	userID := c.GetString("userID")
	instances, err := h.service.ListInstances(userID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// Ensure empty slice instead of null
	if instances == nil {
		instances = []*domain.Instance{}
	}

	SendSuccess(c, http.StatusOK, "Instances retrieved successfully", instances)
}

func (h *InstanceHandler) StopInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	if err := h.service.StopInstance(id, userID); err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "instance stopped", nil)
}

func (h *InstanceHandler) StartInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	if err := h.service.StartInstance(id, userID); err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "instance started", nil)
}

func (h *InstanceHandler) DeleteInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	if err := h.service.DeleteInstance(id, userID); err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "instance terminated", nil)
}
func (h *InstanceHandler) GetStatusChecks(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	checks, err := h.service.GetStatusChecks(id, userID)
	if err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Status checks retrieved successfully", checks)
}

func (h *InstanceHandler) GetMetrics(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	metrics, err := h.service.GetMetrics(id, userID)
	if err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Metrics retrieved successfully", metrics)
}

func (h *InstanceHandler) GetTags(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	tags, err := h.service.GetTags(id, userID)
	if err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if tags == nil {
		tags = []*domain.InstanceTag{}
	}

	SendSuccess(c, http.StatusOK, "Tags retrieved successfully", tags)
}

func (h *InstanceHandler) AddOrUpdateTag(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")

	var tag domain.InstanceTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.AddOrUpdateTag(id, userID, &tag); err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Tag added/updated successfully", nil)
}

func (h *InstanceHandler) DeleteTag(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	key := c.Param("key")

	if err := h.service.DeleteTag(id, userID, key); err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Tag deleted successfully", nil)
}

func (h *InstanceHandler) AssignVPC(c *gin.Context) {
	instanceID := c.Param("id")
	userID := c.GetString("userID")

	var req struct {
		VPCID string `json:"vpc_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.AssignVPC(c.Request.Context(), userID, instanceID, req.VPCID); err != nil {
		if err == domain.ErrInstanceNotFound {
			SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "instance moved to new VPC", nil)
}

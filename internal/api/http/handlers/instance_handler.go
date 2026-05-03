package transport

import (
	"fmt"
	"net/http"

	application "ec2-api/internal/application"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/instance"

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
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, fmt.Sprintf("Instance %s has been restarted", instanceID), nil)
}

func (h *InstanceHandler) CreateInstance(c *gin.Context) {
	var req domain.CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}
	userID := c.GetString("userID")
	instance, err := h.service.CreateInstance(&req, userID)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}
	dto.SendSuccess(c, http.StatusAccepted, "Instance creation started", instance)
}

func (h *InstanceHandler) GetInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	instance, err := h.service.GetInstance(id, userID)
	if err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Instance retrieved successfully", instance)
}

func (h *InstanceHandler) ListInstances(c *gin.Context) {
    userID := c.GetString("userID")
    if userID == "" {
        dto.SendError(c, http.StatusUnauthorized, "missing user identity")
        return
    }

    instances, err := h.service.ListInstances(userID)
    if err != nil {
        dto.SendError(c, http.StatusInternalServerError, err.Error())
        return
    }

    if instances == nil {
        instances = []*domain.Instance{}
    }

    dto.SendSuccess(c, http.StatusOK, "Instances retrieved successfully", instances)
}

func (h *InstanceHandler) StopInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	if err := h.service.StopInstance(id, userID); err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "instance stopped", nil)
}

func (h *InstanceHandler) StartInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	if err := h.service.StartInstance(id, userID); err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "instance started", nil)
}

func (h *InstanceHandler) DeleteInstance(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	if err := h.service.DeleteInstance(id, userID); err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "instance terminated", nil)
}
func (h *InstanceHandler) GetStatusChecks(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	checks, err := h.service.GetStatusChecks(id, userID)
	if err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Status checks retrieved successfully", checks)
}

func (h *InstanceHandler) GetMetrics(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	metrics, err := h.service.GetMetrics(id, userID)
	if err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Metrics retrieved successfully", metrics)
}

func (h *InstanceHandler) GetTags(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	tags, err := h.service.GetTags(id, userID)
	if err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if tags == nil {
		tags = []*domain.InstanceTag{}
	}

	dto.SendSuccess(c, http.StatusOK, "Tags retrieved successfully", tags)
}

func (h *InstanceHandler) AddOrUpdateTag(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")

	var tag domain.InstanceTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.AddOrUpdateTag(id, userID, &tag); err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Tag added/updated successfully", nil)
}

func (h *InstanceHandler) DeleteTag(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString("userID")
	key := c.Param("key")

	if err := h.service.DeleteTag(id, userID, key); err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Tag deleted successfully", nil)
}

func (h *InstanceHandler) AssignVPC(c *gin.Context) {
	instanceID := c.Param("id")
	userID := c.GetString("userID")

	var req struct {
		VPCID string `json:"vpc_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.AssignVPC(c.Request.Context(), userID, instanceID, req.VPCID); err != nil {
		if err == dto.ErrInstanceNotFound {
			dto.SendError(c, http.StatusNotFound, "instance not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "instance moved to new VPC", nil)
}

func (h *InstanceHandler) CreateScalingPolicy(c *gin.Context) {
	userID := c.GetString("userID")

	var req domain.ScalingPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.CreateScalingPolicy(c.Request.Context(), userID, &req); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusAccepted, "Scaling policy creation event published", nil)
}

func (h *InstanceHandler) GetScalingPolicies(c *gin.Context) {
	userID := c.GetString("userID")

	policies, err := h.service.GetScalingPolicies(c.Request.Context(), userID)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if policies == nil {
		policies = []domain.ScalingPolicy{}
	}

	dto.SendSuccess(c, http.StatusOK, "Scaling policies retrieved successfully", policies)
}

func (h *InstanceHandler) UpdateScalingPolicy(c *gin.Context) {
	userID := c.GetString("userID")
	policyID := c.Param("id")

	var req domain.UpdateScalingPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.UpdateScalingPolicy(c.Request.Context(), userID, policyID, &req); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusAccepted, "Scaling policy update event published", nil)
}

func (h *InstanceHandler) DeleteScalingPolicy(c *gin.Context) {
	userID := c.GetString("userID")
	policyID := c.Param("id")

	if err := h.service.DeleteScalingPolicy(c.Request.Context(), userID, policyID); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusAccepted, "Scaling policy delete event published", nil)
}

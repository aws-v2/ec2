package transport

import (
	"net/http"
	"strconv"

	"ec2-api/internal/application"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/instance"


	"github.com/gin-gonic/gin"
)

type SnapshotHandler struct {
	service *application.SnapshotService
}

func NewSnapshotHandler(service *application.SnapshotService) *SnapshotHandler {
	return &SnapshotHandler{service: service}
}

func (h *SnapshotHandler) CreateSnapshot(c *gin.Context) {
	instanceID := c.Param("id")
	var req domain.CreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If body is empty, just use default values
		req = domain.CreateSnapshotRequest{}
	}

	snapshot, err := h.service.CreateSnapshot(instanceID, &req)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusAccepted, "Snapshot creation started", snapshot)
}

func (h *SnapshotHandler) ListSnapshots(c *gin.Context) {
	instanceID := c.Query("instance_id")
	var snapshots []*domain.Snapshot
	var err error

	if instanceID != "" {
		snapshots, err = h.service.ListSnapshotsByInstance(instanceID)
	} else {
		snapshots, err = h.service.ListSnapshots()
	}

	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if snapshots == nil {
		snapshots = []*domain.Snapshot{}
	}

	dto.SendSuccess(c, http.StatusOK, "Snapshots retrieved successfully", snapshots)
}

func (h *SnapshotHandler) GetSnapshot(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid snapshot id")
		return
	}

	snapshot, err := h.service.GetSnapshot(id)
	if err != nil {
		if err == domain.ErrSnapshotNotFound {
			dto.SendError(c, http.StatusNotFound, "snapshot not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Snapshot retrieved successfully", snapshot)
}

func (h *SnapshotHandler) DeleteSnapshot(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid snapshot id")
		return
	}

	if err := h.service.DeleteSnapshot(id); err != nil {
		if err == domain.ErrSnapshotNotFound {
			dto.SendError(c, http.StatusNotFound, "snapshot not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "snapshot deleted", nil)
}

package transport

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"ec2-api/internal/application"
	domain "ec2-api/internal/domain/instance"
	"ec2-api/internal/vpcpkg"

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
		log.Printf("[Handler:CreateSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to create snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Snapshot creation started", snapshot)
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
		log.Printf("[Handler:ListSnapshots] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to list snapshots"))
		return
	}

	if snapshots == nil {
		snapshots = []*domain.Snapshot{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshots retrieved successfully", snapshots)
}

func (h *SnapshotHandler) GetSnapshot(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		log.Printf("[Handler:GetSnapshot] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid snapshot id"))
		return
	}

	snapshot, err := h.service.GetSnapshot(id)
	if err != nil {
		if err == domain.ErrSnapshotNotFound {
			log.Printf("[Handler:GetSnapshot] not found: %v", err)
			vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("snapshot not found"))
			return
		}
		log.Printf("[Handler:GetSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshot retrieved successfully", snapshot)
}

func (h *SnapshotHandler) DeleteSnapshot(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		log.Printf("[Handler:DeleteSnapshot] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid snapshot id"))
		return
	}

	if err := h.service.DeleteSnapshot(id); err != nil {
		if err == domain.ErrSnapshotNotFound {
			log.Printf("[Handler:DeleteSnapshot] not found: %v", err)
			vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("snapshot not found"))
			return
		}
		log.Printf("[Handler:DeleteSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "snapshot deleted", nil)
}

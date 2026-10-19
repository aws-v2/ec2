package transport

import (
	"fmt"
	"log"
	"net/http"

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
	userId:= c.GetString("userID")
	var req domain.CreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If body is empty, just use default values
		req = domain.CreateSnapshotRequest{}
	}
	req.UserID =userId  
	snapshot, err := h.service.CreateSnapshot(instanceID, &req)
	if err != nil {
		log.Printf("[Handler:CreateSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to create snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Snapshot creation started", snapshot)
}

func (h *SnapshotHandler) ClearSnapshot(c *gin.Context) {
	snapshotId := c.Query("id")
	if snapshotId == "" {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("snapshot id is required"))
		return
	}

	err := h.service.ClearSnapshot(snapshotId)
	if err != nil {
		log.Printf("[Handler:ClearSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshot deleted successfully", nil)
}
func (h *SnapshotHandler) ListSnapshotsForUser(c *gin.Context) {
	userId := c.GetString("userID")
	snapshots, err := h.service.ListSnapshots(userId)

	if err != nil {
		log.Printf("[Handler:ListSnapshots] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to list snapshots"))
		return
	}

	if snapshots == nil {
		snapshots = &domain.AllSnapshots{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshots retrieved successfully", snapshots)
}
func (h *SnapshotHandler) ListSnapshots(c *gin.Context) {
	userId := c.GetString("userID")

	instanceID := c.Param("id")
	var snapshots *domain.AllSnapshots
	var err error

	if instanceID != "" {
		log.Printf("listing snapshots for instance %s", instanceID)
		snapshots, err = h.service.ListSnapshotsByInstance(instanceID)
	} else {
		log.Printf("listing snapshots not for instance")

		snapshots, err = h.service.ListSnapshots(userId)
	}

	if err != nil {
		log.Printf("[Handler:ListSnapshots] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to list snapshots"))
		return
	}

	if snapshots == nil {
		snapshots = &domain.AllSnapshots{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshots retrieved successfully", snapshots)
}

func (h *SnapshotHandler) RevertSnapshot(c *gin.Context) {
	snapshotId := c.Param("id")
	if snapshotId == "" {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("snapshot id is required"))
		return
	}

	err := h.service.RevertSnapshot(snapshotId)
	if err != nil {
		log.Printf("[Handler:RevertSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to revert snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshot reverted successfully", nil)
}

func (h *SnapshotHandler) GetSnapshot(c *gin.Context) {
	id := c.Param("id")

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
	id := c.Param("id")

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

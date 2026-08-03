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

type VolumeHandler struct {
	service         *application.VolumeService
	snapshotService *application.SnapshotService
}

func NewVolumeHandler(service *application.VolumeService, snapshotService *application.SnapshotService) *VolumeHandler {
	return &VolumeHandler{
		service:         service,
		snapshotService: snapshotService,
	}
}

func (h *VolumeHandler) CreateVolume(c *gin.Context) {
	var req domain.CreateVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:CreateVolume] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	volume, err := h.service.CreateVolume(&req)
	if err != nil {
		log.Printf("[Handler:CreateVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to create volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Volume creation started", volume)
}

func (h *VolumeHandler) ListVolumes(c *gin.Context) {
	volumes, err := h.service.ListVolumes()
	if err != nil {
		log.Printf("[Handler:ListVolumes] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to list volumes"))
		return
	}

	// Ensure empty slice instead of null
	if volumes == nil {
		volumes = []*domain.Volume{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volumes retrieved successfully", volumes)
}

func (h *VolumeHandler) GetVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:GetVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	volume, err := h.service.GetVolume(volumeID)
	if err != nil {
		log.Printf("[Handler:GetVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("volume not found"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume retrieved successfully", volume)
}

func (h *VolumeHandler) AttachVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:AttachVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	var req domain.AttachVolumeRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:AttachVolume] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}
	volume, err := h.service.AttachVolume(volumeID, req.InstanceID)
	if err != nil {
		log.Printf("[Handler:AttachVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to attach volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume attached successfully", volume)
}

func (h *VolumeHandler) DetachVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:DetachVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	volume, err := h.service.DetachVolume(volumeID)
	if err != nil {
		log.Printf("[Handler:DetachVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to detach volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume detached successfully", volume)
}

func (h *VolumeHandler) ReserveVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:ReserveVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	volume, err := h.service.ReserveVolume(volumeID)
	if err != nil {
		log.Printf("[Handler:ReserveVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to reserve volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Storage_Reservation_Created", volume)
}

func (h *VolumeHandler) ExpandVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:ExpandVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	type ExpandRequest struct {
		NewSize int `json:"new_size" binding:"required,min=1"`
	}

	var req ExpandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:ExpandVolume] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	volume, err := h.service.ExpandVolume(volumeID, req.NewSize)
	if err != nil {
		log.Printf("[Handler:ExpandVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to expand volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Storage_Expansion_Cycles_Started", volume)
}

func (h *VolumeHandler) DeleteVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:DeleteVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	force := c.Query("force") == "true"

	err = h.service.DeleteVolume(volumeID, force)
	if err != nil {
		log.Printf("[Handler:DeleteVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume deleted successfully", nil)
}

func (h *VolumeHandler) CreateSnapshot(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:CreateSnapshot] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	var req domain.CreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If body is empty, just use default values
		req = domain.CreateSnapshotRequest{}
	}

	snapshot, err := h.snapshotService.CreateVolumeSnapshot(volumeID, &req)
	if err != nil {
		log.Printf("[Handler:CreateSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to create snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Snapshot creation started", snapshot)
}

func (h *VolumeHandler) ListSnapshots(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:ListSnapshots] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	snapshots, err := h.snapshotService.ListSnapshotsByVolume(volumeID)
	if err != nil {
		log.Printf("[Handler:ListSnapshots] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to list snapshots"))
		return
	}

	if snapshots == nil {
		snapshots = []*domain.VolumeSnapshot{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Snapshots retrieved successfully", snapshots)
}

func (h *VolumeHandler) DeleteVolumeSnapshot(c *gin.Context) {
	snapshotIDStr := c.Param("id")
	snapshotID, err := strconv.Atoi(snapshotIDStr)
	if err != nil {
		log.Printf("[Handler:DeleteVolumeSnapshot] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid snapshot id"))
		return
	}
	if err := h.snapshotService.DeleteVolumeSnapshot(snapshotID); err != nil {
		if err == domain.ErrSnapshotNotFound {
			log.Printf("[Handler:DeleteVolumeSnapshot] not found: %v", err)
			vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("snapshot not found"))
			return
		}
		log.Printf("[Handler:DeleteVolumeSnapshot] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume snapshot deleted successfully", nil)
}

func (h *VolumeHandler) ListTags(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:ListTags] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	tags, err := h.service.GetTags(volumeID)
	if err != nil {
		log.Printf("[Handler:ListTags] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get tags"))
		return
	}

	if tags == nil {
		tags = []*domain.VolumeTag{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Tags retrieved successfully", tags)
}

func (h *VolumeHandler) AddTag(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:AddTag] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	var tag domain.VolumeTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		log.Printf("[Handler:AddTag] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid tag data"))
		return
	}

	if err := h.service.AddOrUpdateTag(volumeID, &tag); err != nil {
		log.Printf("[Handler:AddTag] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to add or update tag"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Tag added/updated successfully", nil)
}

func (h *VolumeHandler) DeleteTag(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:DeleteTag] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	key := c.Param("key")
	if key == "" {
		log.Printf("[Handler:DeleteTag] missing key")
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("tag key is required"))
		return
	}

	if err := h.service.DeleteTag(volumeID, key); err != nil {
		log.Printf("[Handler:DeleteTag] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete tag"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Tag deleted successfully", nil)
}

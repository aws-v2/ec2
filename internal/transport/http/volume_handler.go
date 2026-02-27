package transport

import (
	"net/http"
	"strconv"

	"github.com/Qarani-m/ec2-api/internal/application"
	"github.com/Qarani-m/ec2-api/internal/domain"
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
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	volume, err := h.service.CreateVolume(&req)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusAccepted, "Volume creation started", volume)
}

func (h *VolumeHandler) ListVolumes(c *gin.Context) {
	volumes, err := h.service.ListVolumes()
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// Ensure empty slice instead of null
	if volumes == nil {
		volumes = []*domain.Volume{}
	}

	SendSuccess(c, http.StatusOK, "Volumes retrieved successfully", volumes)
}

func (h *VolumeHandler) GetVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}
	volume, err := h.service.GetVolume(volumeID)
	if err != nil {
		SendError(c, http.StatusNotFound, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Volume retrieved successfully", volume)
}

func (h *VolumeHandler) AttachVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}
	var req domain.AttachVolumeRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}
	volume, err := h.service.AttachVolume(volumeID, req.InstanceID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Volume attached successfully", volume)
}

func (h *VolumeHandler) DetachVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	volume, err := h.service.DetachVolume(volumeID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Volume detached successfully", volume)
}

func (h *VolumeHandler) ReserveVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	volume, err := h.service.ReserveVolume(volumeID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Storage_Reservation_Created", volume)
}

func (h *VolumeHandler) ExpandVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	type ExpandRequest struct {
		NewSize int `json:"new_size" binding:"required,min=1"`
	}

	var req ExpandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	volume, err := h.service.ExpandVolume(volumeID, req.NewSize)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusAccepted, "Storage_Expansion_Cycles_Started", volume)
}

func (h *VolumeHandler) DeleteVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	force := c.Query("force") == "true"

	err = h.service.DeleteVolume(volumeID, force)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Volume deleted successfully", nil)
}

func (h *VolumeHandler) CreateSnapshot(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	var req domain.CreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If body is empty, just use default values
		req = domain.CreateSnapshotRequest{}
	}

	snapshot, err := h.snapshotService.CreateVolumeSnapshot(volumeID, &req)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusAccepted, "Snapshot creation started", snapshot)
}

func (h *VolumeHandler) ListSnapshots(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	snapshots, err := h.snapshotService.ListSnapshotsByVolume(volumeID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if snapshots == nil {
		snapshots = []*domain.VolumeSnapshot{}
	}

	SendSuccess(c, http.StatusOK, "Snapshots retrieved successfully", snapshots)
}

func (h *VolumeHandler) DeleteVolumeSnapshot(c *gin.Context) {
	snapshotIDStr := c.Param("id")
	snapshotID, err := strconv.Atoi(snapshotIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid snapshot id")
		return
	}

	if err := h.snapshotService.DeleteVolumeSnapshot(snapshotID); err != nil {
		if err == domain.ErrSnapshotNotFound {
			SendError(c, http.StatusNotFound, "snapshot not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Volume snapshot deleted successfully", nil)
}

func (h *VolumeHandler) ListTags(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	tags, err := h.service.GetTags(volumeID)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if tags == nil {
		tags = []*domain.VolumeTag{}
	}

	SendSuccess(c, http.StatusOK, "Tags retrieved successfully", tags)
}

func (h *VolumeHandler) AddTag(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	var tag domain.VolumeTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		SendError(c, http.StatusBadRequest, "invalid tag data")
		return
	}

	if err := h.service.AddOrUpdateTag(volumeID, &tag); err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Tag added/updated successfully", nil)
}

func (h *VolumeHandler) DeleteTag(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid volume id")
		return
	}

	key := c.Param("key")
	if key == "" {
		SendError(c, http.StatusBadRequest, "tag key is required")
		return
	}

	if err := h.service.DeleteTag(volumeID, key); err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Tag deleted successfully", nil)
}

package transport

import (
	"fmt"
	"net/http"
	"strconv"

	"ec2-api/internal/application"
	"ec2-api/internal/domain"

	"github.com/gin-gonic/gin"
)

type SSHKeyHandler struct {
	service *application.SSHKeyService
}

func NewSSHKeyHandler(service *application.SSHKeyService) *SSHKeyHandler {
	return &SSHKeyHandler{service: service}
}

func (h *SSHKeyHandler) CreateSSHKey(c *gin.Context) {
	var req domain.CreateSSHKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	key, err := h.service.CreateSSHKey(&req)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusCreated, "SSH key created successfully", key)
}

func (h *SSHKeyHandler) ListSSHKeys(c *gin.Context) {
	keys, err := h.service.ListSSHKeys()
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if keys == nil {
		keys = []*domain.SSHKey{}
	}

	SendSuccess(c, http.StatusOK, "SSH keys retrieved successfully", keys)
}

func (h *SSHKeyHandler) DeleteSSHKey(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid ssh key id")
		return
	}

	if err := h.service.DeleteSSHKey(id); err != nil {
		if err == domain.ErrSSHKeyNotFound {
			SendError(c, http.StatusNotFound, "ssh key not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "ssh key invalidated successfully", nil)
}
func (h *SSHKeyHandler) DownloadSSHKey(c *gin.Context) {
	name := c.Param("name")

	keyBytes, err := h.service.GetPrivateKeyByName(name)
	if err != nil {
		SendError(c, http.StatusNotFound, "Private key not found or unavailable")
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.pem", name))
	c.Header("Content-Type", "application/x-pem-file")
	c.Data(http.StatusOK, "application/x-pem-file", keyBytes)
}

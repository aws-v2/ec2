package transport

import (
	"net/http"
	"strconv"

	"github.com/Qarani-m/ec2-api/internal/application"
	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/gin-gonic/gin"
)

type TemplateHandler struct {
	service *application.TemplateService
}

func NewTemplateHandler(service *application.TemplateService) *TemplateHandler {
	return &TemplateHandler{service: service}
}

func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	var req domain.CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	template, err := h.service.CreateTemplate(&req)
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusCreated, "Template created successfully", template)
}

func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	templates, err := h.service.ListTemplates()
	if err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if templates == nil {
		templates = []*domain.Template{}
	}

	SendSuccess(c, http.StatusOK, "Templates retrieved successfully", templates)
}

func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid template id")
		return
	}

	template, err := h.service.GetTemplate(id)
	if err != nil {
		if err == domain.ErrTemplateNotFound {
			SendError(c, http.StatusNotFound, "template not found")
			return
		}
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Template retrieved successfully", template)
}

func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		SendError(c, http.StatusBadRequest, "invalid template id")
		return
	}

	if err := h.service.DeleteTemplate(id); err != nil {
		SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	SendSuccess(c, http.StatusOK, "Template deleted successfully", nil)
}

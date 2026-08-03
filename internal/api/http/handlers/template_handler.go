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

type TemplateHandler struct {
	service *application.TemplateService
}

func NewTemplateHandler(service *application.TemplateService) *TemplateHandler {
	return &TemplateHandler{service: service}
}

func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	var req domain.CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:CreateTemplate] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	template, err := h.service.CreateTemplate(&req)
	if err != nil {
		log.Printf("[Handler:CreateTemplate] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to create template"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusCreated, "Template created successfully", template)
}

func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	templates, err := h.service.ListTemplates()
	if err != nil {
		log.Printf("[Handler:ListTemplates] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to list templates"))
		return
	}

	if templates == nil {
		templates = []*domain.Template{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Templates retrieved successfully", templates)
}

func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		log.Printf("[Handler:GetTemplate] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid template id"))
		return
	}

	template, err := h.service.GetTemplate(id)
	if err != nil {
		if err == domain.ErrTemplateNotFound {
			log.Printf("[Handler:GetTemplate] not found: %v", err)
			vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("template not found"))
			return
		}
		log.Printf("[Handler:GetTemplate] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get template"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Template retrieved successfully", template)
}

func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		log.Printf("[Handler:DeleteTemplate] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid template id"))
		return
	}

	if err := h.service.DeleteTemplate(id); err != nil {
		log.Printf("[Handler:DeleteTemplate] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete template"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Template deleted successfully", nil)
}

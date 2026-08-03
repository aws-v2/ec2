package transport

import (
	"ec2-api/internal/application"
	"ec2-api/internal/vpcpkg"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type DocsHandler struct {
	service *application.DocsService
}

func NewDocsHandler(service *application.DocsService) *DocsHandler {
	return &DocsHandler{service: service}
}
func (h *DocsHandler) GetManifest(c *gin.Context) {
	role := c.GetString("role")
	// TODO the role is showing up as empty in some contexts
	fmt.Printf("the role::%v\n", role)

	requestID := c.GetString("requestID")

	if role == "USER" {
		data, err := h.service.GetManifest(false)
		if err != nil {
			log.Printf("[Handler:GetManifest] Service call, requestID %s  error %s", requestID, err.Error())
			vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get public docs manifest"))
			return
		}

		vpcpkg.RespondSucces(c, http.StatusOK, "Fetched documents successfully", gin.H{
			"service":    data.Service,
			"apiVersion": data.APIVersion,
			"scope":      "public",
			"internal":   []application.DocCategory{},
			"public":     data.Public,
		})
		return
	}

	// For administrative/internal roles, return both public and internal manifests
	publicData, err := h.service.GetManifest(false)
	if err != nil {
		log.Printf("[Handler:GetManifest] Service call, requestID %s  error %s", requestID, err.Error())
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get public docs manifest"))
		return
	}

	internalData, err := h.service.GetManifest(true)
	if err != nil {
		log.Printf("[Handler:GetManifest] Service call, requestID %s  error %s", requestID, err.Error())
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get private docs manifest"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Fetched documents successfully", gin.H{
		"service":    chooseString(publicData, internalData),
		"apiVersion": chooseVersion(publicData, internalData),
		"scope":      "internal",
		"internal":   safeCategories(internalData),
		"public":     safeCategories(publicData),
	})
}

func (h *DocsHandler) GetDoc(c *gin.Context) {
	slug := c.Param("slug")
	role := c.GetString("role")
	requestID := c.GetString("requestID")
	if role == "USER" {
		doc, err := h.service.GetDoc(slug, false)
		if err != nil {
			log.Printf("[Handler:GetDoc] Service call, requestID %s  error %s", requestID, err.Error())
			vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("document not found"))
			return
		}

		vpcpkg.RespondSucces(c, http.StatusOK, "Fetched document successfully", doc)
		return
	}

	// First try internal doc, fallback to public doc if not found
	doc, err := h.service.GetDoc(slug, true)
	if err != nil {
		doc, err = h.service.GetDoc(slug, false)
		if err != nil {
			log.Printf("[Handler:GetDoc] Service call, requestID %s  error %s", requestID, err.Error())
			vpcpkg.RespondError(c, http.StatusNotFound, fmt.Errorf("document not found"))
			return
		}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Fetched document successfully", doc)
}

// Helpers to safely extract fields when one of the manifests might be nil
func safeCategories(m *application.DocManifest) []application.DocCategory {
	if m == nil {
		return []application.DocCategory{}
	}
	// prefer Public slice if present, otherwise Internal
	if len(m.Public) > 0 {
		return m.Public
	}
	if len(m.Internal) > 0 {
		return m.Internal
	}
	return []application.DocCategory{}
}

func chooseString(a, b *application.DocManifest) string {
	if a != nil && a.Service != "" {
		return a.Service
	}
	if b != nil {
		return b.Service
	}
	return ""
}

func chooseVersion(a, b *application.DocManifest) string {
	if a != nil && a.APIVersion != "" {
		return a.APIVersion
	}
	if b != nil {
		return b.APIVersion
	}
	return ""
}

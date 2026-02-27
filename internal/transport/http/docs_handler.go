package transport

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// DocsHandler serves the static documentation API.
type DocsHandler struct {
	docsDir string
}

// NewDocsHandler creates a new DocsHandler.
// docsDir is the absolute path to the docs/compute directory.
func NewDocsHandler(docsDir string) *DocsHandler {
	return &DocsHandler{docsDir: docsDir}
}

// manifestResponse is the shape of GET /api/v1/compute/docs
type manifestResponse struct {
	Service    string             `json:"service"`
	Version    string             `json:"version"`
	Categories []manifestCategory `json:"categories"`
}

type manifestCategory struct {
	Title string         `json:"title"`
	Items []manifestItem `json:"items"`
}

type manifestItem struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

// GetManifest returns the docs table of contents.
func (h *DocsHandler) GetManifest(c *gin.Context) {
	manifestPath := filepath.Join(h.docsDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		SendError(c, http.StatusInternalServerError, "Failed to load docs manifest")
		return
	}

	var manifest manifestResponse
	if err := json.Unmarshal(data, &manifest); err != nil {
		SendError(c, http.StatusInternalServerError, "Failed to parse docs manifest")
		return
	}

	SendSuccess(c, http.StatusOK, "Docs manifest retrieved successfully", manifest)
}

// docMetadata is the per-doc metadata stored in metadata.json
type docMetadata struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	LastUpdated string   `json:"lastUpdated"`
	Tags        []string `json:"tags"`
}

// docResponse is the shape of GET /api/v1/compute/docs/:slug
type docResponse struct {
	Metadata docMetadata `json:"metadata"`
	Content  string      `json:"content"`
}

// GetDocBySlug returns the full documentation page for a given slug.
func (h *DocsHandler) GetDocBySlug(c *gin.Context) {
	slug := c.Param("slug")

	// Load metadata
	metadataPath := filepath.Join(h.docsDir, "metadata.json")
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		SendError(c, http.StatusInternalServerError, "Failed to load docs metadata")
		return
	}

	var allMetadata map[string]docMetadata
	if err := json.Unmarshal(metadataBytes, &allMetadata); err != nil {
		SendError(c, http.StatusInternalServerError, "Failed to parse docs metadata")
		return
	}

	meta, ok := allMetadata[slug]
	if !ok {
		SendError(c, http.StatusNotFound, "Documentation page not found")
		return
	}

	// Load markdown content
	mdPath := filepath.Join(h.docsDir, slug+".md")
	contentBytes, err := os.ReadFile(mdPath)
	if err != nil {
		SendError(c, http.StatusNotFound, "Documentation content not found")
		return
	}

	resp := docResponse{
		Metadata: meta,
		Content:  string(contentBytes),
	}

	SendSuccess(c, http.StatusOK, "Documentation retrieved successfully", resp)
}

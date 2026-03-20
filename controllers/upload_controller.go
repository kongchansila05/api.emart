package controllers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"project-api/services"
)

// UploadController handles generic file upload endpoints.
type UploadController struct {
	r2 *services.R2Service
}

func NewUploadController(r2 *services.R2Service) *UploadController {
	return &UploadController{r2: r2}
}

var allowedMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// UploadImage godoc
// @Summary  Upload an image to R2 (returns public URL)
// @Tags     Upload
// @Security BearerAuth
// @Accept   multipart/form-data
// @Produce  json
// @Param    file   formData file   true "Image file"
// @Param    folder formData string false "Destination folder (default: uploads)"
// @Success  200 {object} map[string]string
// @Failure  400,500 {object} map[string]string
// @Router   /admin/upload [post]
func (uc *UploadController) UploadImage(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file provided"})
		return
	}

	// validate mime type
	mime := fh.Header.Get("Content-Type")
	if !allowedMimes[strings.ToLower(mime)] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only JPEG, PNG, WebP and GIF are allowed"})
		return
	}

	// validate size (max 5 MB)
	const maxSize = 5 << 20
	if fh.Size > maxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File too large (max 5 MB)"})
		return
	}

	folder := c.PostForm("folder")
	if folder == "" {
		folder = "uploads"
	}

	url, err := uc.r2.UploadFile(fh, folder)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Upload failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}
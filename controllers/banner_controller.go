package controllers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"project-api/models"
	"project-api/utils"
)

type BannerController struct {
	db *gorm.DB
}

func NewBannerController(db *gorm.DB) *BannerController {
	return &BannerController{db: db}
}

func bannerIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid banner ID"})
		return 0, false
	}
	return uint(id), true
}

// ─── Public ───────────────────────────────────────────────────────────────────

// GetActive — returns active banners within their schedule.
// GET /banners?position=top
func (bc *BannerController) GetActive(c *gin.Context) {
	now := time.Now()
	q := bc.db.Where("is_active = ?", true).
		Where("(starts_at IS NULL OR starts_at <= ?)", now).
		Where("(ends_at IS NULL OR ends_at >= ?)", now).
		Order("sort_order ASC, created_at DESC")

	if pos := c.Query("position"); pos != "" {
		q = q.Where("position = ?", pos)
	}

	var banners []models.Banner
	q.Find(&banners)
	c.JSON(http.StatusOK, banners)
}

// ─── Admin ────────────────────────────────────────────────────────────────────

// AdminGetAll — paginated list.
func (bc *BannerController) AdminGetAll(c *gin.Context) {
	scope := bc.db.Model(&models.Banner{}).Order("sort_order ASC, created_at DESC")

	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		scope = scope.Where("title LIKE ?", like)
	}
	if pos := c.Query("position"); pos != "" {
		scope = scope.Where("position = ?", pos)
	}
	if status := c.Query("is_active"); status != "" {
		scope = scope.Where("is_active = ?", status == "true")
	}

	result, err := utils.Paginate[models.Banner](scope, c, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminCreate — create a new banner.
func (bc *BannerController) AdminCreate(c *gin.Context) {
	var req struct {
		Title     string  `json:"title"      binding:"required"`
		Image     string  `json:"image"      binding:"required"`
		LinkURL   string  `json:"link_url"`
		Position  string  `json:"position"`
		SortOrder int     `json:"sort_order"`
		IsActive  *bool   `json:"is_active"`
		StartsAt  *string `json:"starts_at"`
		EndsAt    *string `json:"ends_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	position := models.BannerPositionTop
	if req.Position != "" {
		position = models.BannerPosition(req.Position)
	}

	banner := models.Banner{
		Title:     req.Title,
		Image:     req.Image,
		LinkURL:   req.LinkURL,
		Position:  position,
		SortOrder: req.SortOrder,
		IsActive:  isActive,
		StartsAt:  parseTime(req.StartsAt),
		EndsAt:    parseTime(req.EndsAt),
	}

	if err := bc.db.Create(&banner).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, banner)
}

// AdminUpdate — update a banner.
func (bc *BannerController) AdminUpdate(c *gin.Context) {
	id, ok := bannerIDParam(c)
	if !ok {
		return
	}

	var banner models.Banner
	if err := bc.db.First(&banner, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Banner not found"})
		return
	}

	var req struct {
		Title     string  `json:"title"`
		Image     string  `json:"image"`
		LinkURL   string  `json:"link_url"`
		Position  string  `json:"position"`
		SortOrder *int    `json:"sort_order"`
		IsActive  *bool   `json:"is_active"`
		StartsAt  *string `json:"starts_at"`
		EndsAt    *string `json:"ends_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if req.Title    != "" { updates["title"]      = req.Title }
	if req.Image    != "" { updates["image"]      = req.Image }
	if req.LinkURL  != "" { updates["link_url"]   = req.LinkURL }
	if req.Position != "" { updates["position"]   = req.Position }
	if req.IsActive  != nil { updates["is_active"]  = *req.IsActive }
	if req.SortOrder != nil { updates["sort_order"] = *req.SortOrder }
	if req.StartsAt  != nil { updates["starts_at"]  = parseTime(req.StartsAt) }
	if req.EndsAt    != nil { updates["ends_at"]    = parseTime(req.EndsAt) }

	bc.db.Model(&banner).Updates(updates)
	c.JSON(http.StatusOK, banner)
}

// AdminToggleActive — flip is_active.
func (bc *BannerController) AdminToggleActive(c *gin.Context) {
	id, ok := bannerIDParam(c)
	if !ok {
		return
	}
	var banner models.Banner
	if err := bc.db.First(&banner, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Banner not found"})
		return
	}
	bc.db.Model(&banner).Update("is_active", !banner.IsActive)
	c.JSON(http.StatusOK, gin.H{"is_active": !banner.IsActive})
}

// AdminDelete — delete a banner.
func (bc *BannerController) AdminDelete(c *gin.Context) {
	id, ok := bannerIDParam(c)
	if !ok {
		return
	}
	bc.db.Delete(&models.Banner{}, id)
	c.JSON(http.StatusOK, gin.H{"message": "Banner deleted"})
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02T15:04", *s)
	if err != nil {
		t, err = time.Parse("2006-01-02", *s)
		if err != nil {
			return nil
		}
	}
	return &t
}
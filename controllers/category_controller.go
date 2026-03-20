package controllers

import (
	"net/http"
	"strconv"
    "fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"project-api/models"
	"project-api/services"
	"project-api/utils"
)

type CategoryController struct {
	db *gorm.DB
	r2 *services.R2Service
}

func NewCategoryController(db *gorm.DB, r2 *services.R2Service) *CategoryController {
	return &CategoryController{db: db, r2: r2}
}

// ─── DTOs ─────────────────────────────────────────────────────────────────────

type categoryRequest struct {
	Name        string `json:"name"        binding:"required"`
	Description string `json:"description"`
	Image       string `json:"image"`
	IsActive    *bool  `json:"is_active"`
}

type subCategoryRequest struct {
	Name        string `json:"name"        binding:"required"`
	Description string `json:"description"`
	Image       string `json:"image"`
	IsActive    *bool  `json:"is_active"`
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func catIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return 0, false
	}
	return uint(id), true
}

func subCatIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("sub_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid subcategory ID"})
		return 0, false
	}
	return uint(id), true
}


func (cc *CategoryController) attachPostCounts(cats []models.Category) {
	for i := range cats {
		var count int64
		cc.db.Model(&models.Post{}).
			Where("category_id = ? AND status = ?", cats[i].ID, models.StatusActive).
			Count(&count)
		cats[i].PostCount = int(count)

		// Also attach post count per subcategory
		for j := range cats[i].SubCategories {
			var subCount int64
			cc.db.Model(&models.Post{}).
				Where("sub_category_id = ? AND status = ?", cats[i].SubCategories[j].ID, models.StatusActive).
				Count(&subCount)
			cats[i].SubCategories[j].PostCount = int(subCount)
		}
	}
}

func (cc *CategoryController) attachSubCategoryPostCounts(subs []models.SubCategory) {
	for i := range subs {
		var count int64
		cc.db.Model(&models.Post{}).
			Where("sub_category_id = ? AND status = ?", subs[i].ID, models.StatusActive).
			Count(&count)
		subs[i].PostCount = int(count)
	}
}

// ─── Public ───────────────────────────────────────────────────────────────────

// GetActive — public list of active categories with their active subcategories
func (cc *CategoryController) GetActive(c *gin.Context) {
	scope := cc.db.Model(&models.Category{}).
		Preload("SubCategories", "is_active = ?", true).
		Where("is_active = ?", true).
		Order("created_at DESC")

	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		scope = scope.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	result, err := utils.Paginate[models.Category](scope, c, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cc.attachPostCounts(result.Data)
	c.JSON(http.StatusOK, result)
}

// GetSubCategories — public list of active subcategories for a category
// GET /categories/:id/sub-categories
func (cc *CategoryController) GetSubCategories(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}
	var subs []models.SubCategory
	cc.db.Where("category_id = ? AND is_active = ?", id, true).
		Order("created_at DESC").
		Find(&subs)
	cc.attachSubCategoryPostCounts(subs)
	c.JSON(http.StatusOK, subs)
}

// ─── Admin Category ───────────────────────────────────────────────────────────

func (cc *CategoryController) AdminGetAll(c *gin.Context) {
	scope := cc.db.Model(&models.Category{}).
		Preload("SubCategories").
		Order("created_at DESC")

	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		scope = scope.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if status := c.Query("is_active"); status != "" {
		scope = scope.Where("is_active = ?", status == "true")
	}

	result, err := utils.Paginate[models.Category](scope, c, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cc.attachPostCounts(result.Data)
	c.JSON(http.StatusOK, result)
}

func (cc *CategoryController) AdminCreate(c *gin.Context) {
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	cat := models.Category{
		Name:        req.Name,
		Description: req.Description,
		Image:       req.Image,
		IsActive:    isActive,
	}
	if err := cc.db.Create(&cat).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Category name already exists"})
		return
	}
	c.JSON(http.StatusCreated, cat)
}

func (cc *CategoryController) AdminUpdate(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}

	var cat models.Category
	if err := cc.db.First(&cat, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	var changes map[string]interface{}
	if err := c.ShouldBindJSON(&changes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if newImage, ok := changes["image"].(string); ok && newImage != cat.Image && cat.Image != "" {
		_ = cc.r2.DeleteFile(cat.Image)
	}

	cc.db.Model(&cat).Updates(changes)
	c.JSON(http.StatusOK, cat)
}

func (cc *CategoryController) AdminDelete(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}

	var cat models.Category
	if err := cc.db.First(&cat, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	// ── Block delete if category has posts ────────────────────────────────────
	var postCount int64
	cc.db.Model(&models.Post{}).Where("category_id = ?", id).Count(&postCount)
	if postCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Cannot delete — this category has %d post(s). Remove or reassign them first.", postCount),
		})
		return
	}

	// ── Block delete if any subcategory has posts ─────────────────────────────
	var subIDs []uint
	cc.db.Model(&models.SubCategory{}).
		Where("category_id = ?", id).
		Pluck("id", &subIDs)

	if len(subIDs) > 0 {
		var subPostCount int64
		cc.db.Model(&models.Post{}).
			Where("sub_category_id IN ?", subIDs).
			Count(&subPostCount)
		if subPostCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Cannot delete — subcategories have %d post(s). Remove or reassign them first.", subPostCount),
			})
			return
		}
	}

	// Safe to delete — remove subcategory images + records first
	var subs []models.SubCategory
	cc.db.Where("category_id = ?", id).Find(&subs)
	for _, s := range subs {
		if s.Image != "" {
			_ = cc.r2.DeleteFile(s.Image)
		}
	}
	cc.db.Where("category_id = ?", id).Delete(&models.SubCategory{})

	if cat.Image != "" {
		_ = cc.r2.DeleteFile(cat.Image)
	}
	cc.db.Delete(&cat)
	c.JSON(http.StatusOK, gin.H{"message": "Category deleted"})
}

// ─── Admin SubCategory ────────────────────────────────────────────────────────

// AdminGetSubCategories — paginated subcategories for a category
// GET /admin/categories/:id/sub-categories
func (cc *CategoryController) AdminGetSubCategories(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}

	scope := cc.db.Model(&models.SubCategory{}).
		Where("category_id = ?", id).
		Order("created_at DESC")

	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		scope = scope.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if status := c.Query("is_active"); status != "" {
		scope = scope.Where("is_active = ?", status == "true")
	}

	result, err := utils.Paginate[models.SubCategory](scope, c, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cc.attachSubCategoryPostCounts(result.Data)
	c.JSON(http.StatusOK, result)
}

// AdminCreateSubCategory — create subcategory under a category
// POST /admin/categories/:id/sub-categories
func (cc *CategoryController) AdminCreateSubCategory(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}

	// Verify parent category exists
	var cat models.Category
	if err := cc.db.First(&cat, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	var req subCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	sub := models.SubCategory{
		CategoryID:  id,
		Name:        req.Name,
		Description: req.Description,
		Image:       req.Image,
		IsActive:    isActive,
	}
	if err := cc.db.Create(&sub).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "SubCategory name already exists in this category"})
		return
	}
	c.JSON(http.StatusCreated, sub)
}

// AdminUpdateSubCategory — update a subcategory
// PUT /admin/categories/:id/sub-categories/:sub_id
func (cc *CategoryController) AdminUpdateSubCategory(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}
	subID, ok := subCatIDParam(c)
	if !ok {
		return
	}

	var sub models.SubCategory
	if err := cc.db.Where("id = ? AND category_id = ?", subID, id).First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SubCategory not found"})
		return
	}

	var changes map[string]interface{}
	if err := c.ShouldBindJSON(&changes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if newImage, ok := changes["image"].(string); ok && newImage != sub.Image && sub.Image != "" {
		_ = cc.r2.DeleteFile(sub.Image)
	}

	cc.db.Model(&sub).Updates(changes)
	c.JSON(http.StatusOK, sub)
}

// AdminDeleteSubCategory — delete a subcategory
// DELETE /admin/categories/:id/sub-categories/:sub_id
func (cc *CategoryController) AdminDeleteSubCategory(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}
	subID, ok := subCatIDParam(c)
	if !ok {
		return
	}

	var sub models.SubCategory
	if err := cc.db.Where("id = ? AND category_id = ?", subID, id).First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SubCategory not found"})
		return
	}

	// ── Block delete if subcategory has posts ─────────────────────────────────
	var postCount int64
	cc.db.Model(&models.Post{}).Where("sub_category_id = ?", subID).Count(&postCount)
	if postCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Cannot delete — this subcategory has %d post(s). Remove or reassign them first.", postCount),
		})
		return
	}

	if sub.Image != "" {
		_ = cc.r2.DeleteFile(sub.Image)
	}
	cc.db.Delete(&sub)
	c.JSON(http.StatusOK, gin.H{"message": "SubCategory deleted"})
}

// AdminToggleSubCategoryStatus — toggle is_active
// PATCH /admin/categories/:id/sub-categories/:sub_id/status
func (cc *CategoryController) AdminToggleSubCategoryStatus(c *gin.Context) {
	id, ok := catIDParam(c)
	if !ok {
		return
	}
	subID, ok := subCatIDParam(c)
	if !ok {
		return
	}

	var sub models.SubCategory
	if err := cc.db.Where("id = ? AND category_id = ?", subID, id).First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SubCategory not found"})
		return
	}

	cc.db.Model(&sub).Update("is_active", !sub.IsActive)
	c.JSON(http.StatusOK, gin.H{"is_active": !sub.IsActive})
}

package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"project-api/middleware"
	"project-api/models"
	"project-api/services"
	"project-api/utils"
)

// UserController exposes HTTP handlers for users, roles, permissions, and stats.
type UserController struct {
	db      *gorm.DB
	postSvc *services.PostService
}

// NewUserController constructs a UserController.
func NewUserController(db *gorm.DB, postSvc *services.PostService) *UserController {
	return &UserController{db: db, postSvc: postSvc}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func userIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return 0, false
	}
	return uint(id), true
}

func roleIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return 0, false
	}
	return uint(id), true
}

func permIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid permission ID"})
		return 0, false
	}
	return uint(id), true
}

// ─── Me ───────────────────────────────────────────────────────────────────────

// GetMe godoc
// @Summary  Return the authenticated user's profile and post count
// @Tags     Users
// @Security BearerAuth
// @Produce  json
// @Success  200 {object} map[string]interface{}
// @Router   /me [get]
func (uc *UserController) GetMe(c *gin.Context) {
	userID := c.GetUint(middleware.CtxUserID)

	var user models.User
	if err := uc.db.Preload("Role.Permissions").First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	user.PostCount = int(uc.postSvc.CountByUser(userID))
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// ─── Admin: Dashboard Stats ───────────────────────────────────────────────────

// GetStats godoc
// @Summary  Admin — platform-wide statistics
// @Tags     Admin / Stats
// @Security BearerAuth
// @Produce  json
// @Success  200 {object} map[string]int64
// @Router   /admin/stats [get]
func (uc *UserController) GetStats(c *gin.Context) {
	var (
		totalUsers, totalPosts      int64
		totalCategories, activePosts int64
		soldPosts                   int64
	)
	uc.db.Model(&models.User{}).Count(&totalUsers)
	uc.db.Model(&models.Post{}).Count(&totalPosts)
	uc.db.Model(&models.Category{}).Count(&totalCategories)
	uc.db.Model(&models.Post{}).Where("status = ?", models.StatusActive).Count(&activePosts)
	uc.db.Model(&models.Post{}).Where("status = ?", models.StatusSold).Count(&soldPosts)

	c.JSON(http.StatusOK, gin.H{
		"total_users":      totalUsers,
		"total_posts":      totalPosts,
		"total_categories": totalCategories,
		"active_posts":     activePosts,
		"sold_posts":       soldPosts,
	})
}

// ─── Admin: Users ─────────────────────────────────────────────────────────────

// AdminGetUsers godoc
// @Summary  Admin — list all users
// @Tags     Admin / Users
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} models.User
// @Router   /admin/users [get]
func (uc *UserController) AdminGetUsers(c *gin.Context) {
	scope := uc.db.Model(&models.User{}).
		Joins("JOIN roles ON roles.id = users.role_id").
		Preload("Role.Permissions").
		Order("users.created_at DESC")

	// Search by name or email
	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		scope = scope.Where("users.name LIKE ? OR users.email LIKE ?", like, like)
	}

	// Filter by exact role name: ?role=client | admin | administrator
	if role := c.Query("role"); role != "" {
		scope = scope.Where("roles.name = ?", role)
	}

	// Exclude client role — used by staff page: ?is_staff=true
	if c.Query("is_staff") == "true" {
		scope = scope.Where("roles.name != ?", "client")
	}

	// Filter by status: ?is_active=true | false | (omit = all)
	if status := c.Query("is_active"); status != "" {
		scope = scope.Where("users.is_active = ?", status == "true")
	}

	result, err := utils.Paginate[models.User](scope, c, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for i := range result.Data {
		result.Data[i].PostCount = int(uc.postSvc.CountByUser(result.Data[i].ID))
	}

	c.JSON(http.StatusOK, result)
}
// AdminUpdateUser godoc
// @Summary  Admin — update user fields (role, post_limit, etc.)
// @Tags     Admin / Users
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    id   path int            true "User ID"
// @Param    body body map[string]any true "Fields to update"
// @Success  200 {object} models.User
// @Failure  404 {object} map[string]string
// @Router   /admin/users/{id} [put]
func (uc *UserController) AdminUpdateUser(c *gin.Context) {
	id, ok := userIDParam(c)
	if !ok {
		return
	}

	var user models.User
	if err := uc.db.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var changes map[string]interface{}
	if err := c.ShouldBindJSON(&changes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Never allow password or email to be changed via this endpoint.
	delete(changes, "password")
	delete(changes, "email")

	if rawPwd, ok := changes["new_password"].(string); ok && rawPwd != "" {
		hash, _ := bcrypt.GenerateFromPassword([]byte(rawPwd), bcrypt.DefaultCost)
		changes["password"] = string(hash)
		delete(changes, "new_password")
	}

	uc.db.Model(&user).Updates(changes)
	uc.db.Preload("Role").First(&user, user.ID)
	c.JSON(http.StatusOK, user)
}

// AdminDeleteUser godoc
// @Summary  Admin — delete a user
// @Tags     Admin / Users
// @Security BearerAuth
// @Param    id path int true "User ID"
// @Success  200 {object} map[string]string
// @Router   /admin/users/{id} [delete]
func (uc *UserController) AdminDeleteUser(c *gin.Context) {
	id, ok := userIDParam(c)
	if !ok {
		return
	}
	uc.db.Delete(&models.User{}, id)
	c.JSON(http.StatusOK, gin.H{"message": "User deleted"})
}

// AdminSetPostLimit godoc
// @Summary  Admin — set a user's post limit
// @Tags     Admin / Users
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    id   path int    true "User ID"
// @Param    body body object true "{ post_limit: int }"
// @Success  200 {object} map[string]interface{}
// @Failure  400,404 {object} map[string]string
// @Router   /admin/users/{id}/limit [patch]
func (uc *UserController) AdminSetPostLimit(c *gin.Context) {
	id, ok := userIDParam(c)
	if !ok {
		return
	}

	var body struct {
		PostLimit int `json:"post_limit" binding:"required,min=0"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := uc.db.Model(&models.User{}).Where("id = ?", id).Update("post_limit", body.PostLimit)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Post limit updated", "post_limit": body.PostLimit})
}

// AdminToggleStatus godoc
// @Summary  Admin — toggle user active/disabled state
// @Tags     Admin / Users
// @Security BearerAuth
// @Param    id path int true "User ID"
// @Success  200 {object} map[string]bool
// @Failure  404 {object} map[string]string
// @Router   /admin/users/{id}/status [patch]
func (uc *UserController) AdminToggleStatus(c *gin.Context) {
	id, ok := userIDParam(c)
	if !ok {
		return
	}

	var user models.User
	if err := uc.db.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	newStatus := !user.IsActive
	uc.db.Model(&user).Update("is_active", newStatus)
	c.JSON(http.StatusOK, gin.H{"is_active": newStatus})
}

// ─── Admin: Roles ─────────────────────────────────────────────────────────────

// GetRoles godoc
// @Summary  Admin — list all roles with their permissions
// @Tags     Admin / Roles
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} models.Role
// @Router   /admin/roles [get]
func (uc *UserController) GetRoles(c *gin.Context) {
	var roles []models.Role
	uc.db.Preload("Permissions").Find(&roles)
	c.JSON(http.StatusOK, roles)
}

// GetStaffRoles godoc
// @Summary  Admin — list all staff roles with their permissions
// @Tags     Admin / Roles
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} models.Role
// @Router   /admin/roles/staff [get]
func (uc *UserController) GetStaffRoles(c *gin.Context) {
	var roles []models.Role

	uc.db.
		Preload("Permissions").
		Where("name NOT IN ?", []string{"administrator", "client"}).
		Find(&roles)

	c.JSON(http.StatusOK, roles)
}

// CreateRole godoc
// @Summary  Admin — create a new role
// @Tags     Admin / Roles
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    body body object true "{ name, description, permissions: [uint] }"
// @Success  201 {object} models.Role
// @Failure  400,409 {object} map[string]string
// @Router   /admin/roles [post]
func (uc *UserController) CreateRole(c *gin.Context) {
	var req struct {
		Name        string `json:"name"        binding:"required"`
		Description string `json:"description"`
		Permissions []uint `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	role := models.Role{Name: req.Name, Description: req.Description}
	if err := uc.db.Create(&role).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Role name already exists"})
		return
	}

	if len(req.Permissions) > 0 {
		var perms []models.Permission
		uc.db.Where("id IN ?", req.Permissions).Find(&perms)
		uc.db.Model(&role).Association("Permissions").Replace(perms)
	}

	uc.db.Preload("Permissions").First(&role, role.ID)
	c.JSON(http.StatusCreated, role)
}

// UpdateRole godoc
// @Summary  Admin — update a role's name, description, or permissions
// @Tags     Admin / Roles
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    id   path int    true "Role ID"
// @Param    body body object true "{ name?, description?, permissions?: [uint] }"
// @Success  200 {object} models.Role
// @Failure  404 {object} map[string]string
// @Router   /admin/roles/{id} [put]
func (uc *UserController) UpdateRole(c *gin.Context) {
	id, ok := roleIDParam(c)
	if !ok {
		return
	}

	var role models.Role
	if err := uc.db.Preload("Permissions").First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Permissions []uint `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != "" {
		uc.db.Model(&role).Update("name", req.Name)
	}
	if req.Description != "" {
		uc.db.Model(&role).Update("description", req.Description)
	}
	if req.Permissions != nil {
		var perms []models.Permission
		uc.db.Where("id IN ?", req.Permissions).Find(&perms)
		uc.db.Model(&role).Association("Permissions").Replace(perms)
	}

	uc.db.Preload("Permissions").First(&role, role.ID)
	c.JSON(http.StatusOK, role)
}

// DeleteRole godoc
// @Summary  Admin — delete a role
// @Tags     Admin / Roles
// @Security BearerAuth
// @Param    id path int true "Role ID"
// @Success  200 {object} map[string]string
// @Failure  404 {object} map[string]string
// @Router   /admin/roles/{id} [delete]
func (uc *UserController) DeleteRole(c *gin.Context) {
	id, ok := roleIDParam(c)
	if !ok {
		return
	}
	result := uc.db.Delete(&models.Role{}, id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Role deleted"})
}

// ─── Admin: Permissions ───────────────────────────────────────────────────────

// GetPermissions godoc
// @Summary  Admin — list all permission keys
// @Tags     Admin / Permissions
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} models.Permission
// @Router   /admin/permissions [get]
func (uc *UserController) GetPermissions(c *gin.Context) {
	var perms []models.Permission
	uc.db.Find(&perms)
	c.JSON(http.StatusOK, perms)
}

// CreatePermission godoc
// @Summary  Admin — add a new permission key
// @Tags     Admin / Permissions
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    body body models.Permission true "Permission payload"
// @Success  201 {object} models.Permission
// @Failure  400,409 {object} map[string]string
// @Router   /admin/permissions [post]
func (uc *UserController) CreatePermission(c *gin.Context) {
	var perm models.Permission
	if err := c.ShouldBindJSON(&perm); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := uc.db.Create(&perm).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Permission key already exists"})
		return
	}
	c.JSON(http.StatusCreated, perm)
}

// DeletePermission godoc
// @Summary  Admin — delete a permission key
// @Tags     Admin / Permissions
// @Security BearerAuth
// @Param    id path int true "Permission ID"
// @Success  200 {object} map[string]string
// @Failure  404 {object} map[string]string
// @Router   /admin/permissions/{id} [delete]
func (uc *UserController) DeletePermission(c *gin.Context) {
	id, ok := permIDParam(c)
	if !ok {
		return
	}
	result := uc.db.Delete(&models.Permission{}, id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Permission not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Permission deleted"})
}
// AdminSetImageLimit godoc
// @Summary  Admin — set a user's image-per-post limit
// @Tags     Admin / Users
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    id   path int    true "User ID"
// @Param    body body object true "{ image_limit: int }"
// @Success  200 {object} map[string]interface{}
// @Failure  400,404 {object} map[string]string
// @Router   /admin/users/{id}/image-limit [patch]
func (uc *UserController) AdminSetImageLimit(c *gin.Context) {
	id, ok := userIDParam(c)
	if !ok {
		return
	}

	var body struct {
		ImageLimit int `json:"image_limit" binding:"required,min=1,max=20"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := uc.db.Model(&models.User{}).Where("id = ?", id).Update("image_limit", body.ImageLimit)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":     "Image limit updated",
		"image_limit": body.ImageLimit,
	})
}
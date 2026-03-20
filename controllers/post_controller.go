package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"project-api/middleware"
	"project-api/models"
	"project-api/services"
	"project-api/utils"
)

type PostController struct {
	svc *services.PostService
}

func NewPostController(svc *services.PostService) *PostController {
	return &PostController{svc: svc}
}

func postIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid post ID"})
		return 0, false
	}
	return uint(id), true
}

// ─── Public ───────────────────────────────────────────────────────────────────

func (pc *PostController) GetAll(c *gin.Context) {
	catID, _ := strconv.ParseUint(c.Query("category_id"), 10, 64)
	posts, err := pc.svc.List(services.PostFilter{
		Search:     c.Query("search"),
		CategoryID: uint(catID),
		Status:     c.Query("status"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Attach like meta for the calling user (0 = guest)
	userID := c.GetUint(middleware.CtxUserID)
	pc.svc.AttachLikeMeta(posts, userID)
	c.JSON(http.StatusOK, posts)
}

func (pc *PostController) GetOne(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}
	post, err := pc.svc.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Post not found"})
		return
	}
	// Increment view every time it is fetched
	pc.svc.IncrementView(id)
	post.ViewCount++

	// Attach like meta
	userID := c.GetUint(middleware.CtxUserID)
	posts := []models.Post{*post}
	pc.svc.AttachLikeMeta(posts, userID)
	post = &posts[0]

	c.JSON(http.StatusOK, post)
}

// ─── Owner ────────────────────────────────────────────────────────────────────

func (pc *PostController) GetMine(c *gin.Context) {
	userID := c.GetUint(middleware.CtxUserID)
	posts, err := pc.svc.List(services.PostFilter{UserID: userID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	pc.svc.AttachLikeMeta(posts, userID)
	c.JSON(http.StatusOK, posts)
}

func (pc *PostController) Create(c *gin.Context) {
	userID := c.GetUint(middleware.CtxUserID)
	var post models.Post
	if err := c.ShouldBindJSON(&post); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	post.UserID = userID
	if err := pc.svc.Create(&post); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, post)
}

func (pc *PostController) Update(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}
	userID := c.GetUint(middleware.CtxUserID)
	var changes map[string]interface{}
	if err := c.ShouldBindJSON(&changes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	post, err := pc.svc.Update(id, userID, changes)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, post)
}

func (pc *PostController) Delete(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}
	userID := c.GetUint(middleware.CtxUserID)
	if err := pc.svc.Delete(id, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Post deleted successfully"})
}

// ─── Admin ────────────────────────────────────────────────────────────────────

// AdminGetAll — paginated with search + status filter
func (pc *PostController) AdminGetAll(c *gin.Context) {
	scope := pc.svc.DB().
		Model(&models.Post{}).
		Preload("User").
		Preload("Category").
		Preload("SubCategory").        // ← add
		Order("posts.created_at DESC")

	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		scope = scope.Where("posts.title LIKE ? OR posts.description LIKE ?", like, like)
	}
	if status := c.Query("status"); status != "" {
		scope = scope.Where("posts.status = ?", status)
	}
	if catID := c.Query("category_id"); catID != "" {
		scope = scope.Where("posts.category_id = ?", catID)
	}
	if subCatID := c.Query("sub_category_id"); subCatID != "" {   // ← add
		scope = scope.Where("posts.sub_category_id = ?", subCatID)
	}

	result, err := utils.Paginate[models.Post](scope, c, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetUint(middleware.CtxUserID)
	pc.svc.AttachLikeMeta(result.Data, userID)
	c.JSON(http.StatusOK, result)
}


// AdminCreatePost — create a post on behalf of a client, with image limit enforcement
func (pc *PostController) AdminCreatePost(c *gin.Context) {
	var req struct {
		UserID        uint     `json:"user_id"         binding:"required"`
		Title         string   `json:"title"           binding:"required"`
		Description   string   `json:"description"`
		Price         float64  `json:"price"           binding:"required,min=0"`
		CategoryID    uint     `json:"category_id"`
		SubCategoryID *uint    `json:"sub_category_id"` // ← add (nullable)
		Images        []string `json:"images"`
		Location      string   `json:"location"`
		Latitude      float64  `json:"latitude"`
		Longitude     float64  `json:"longitude"`
		Condition     string   `json:"condition"`
		Status        string   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	imageLimit, err := pc.getImageLimit(req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if len(req.Images) > imageLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":    "image limit exceeded",
			"limit":    imageLimit,
			"uploaded": len(req.Images),
		})
		return
	}

	status := models.StatusActive
	if req.Status != "" {
		status = models.PostStatus(req.Status)
	}

	post := models.Post{
		UserID:        req.UserID,
		Title:         req.Title,
		Description:   req.Description,
		Price:         req.Price,
		CategoryID:    req.CategoryID,
		SubCategoryID: req.SubCategoryID, // ← add
		Images:        encodeImages(req.Images),
		Location:      req.Location,
		Latitude:      req.Latitude,
		Longitude:     req.Longitude,
		Condition:     req.Condition,
		Status:        status,
	}

	if err := pc.svc.AdminCreate(&post); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, post)
}
// AdminUpdatePost — update any post, with image limit enforcement
func (pc *PostController) AdminUpdatePost(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}

	var req struct {
		Title         string   `json:"title"`
		Description   string   `json:"description"`
		Price         float64  `json:"price"`
		CategoryID    uint     `json:"category_id"`
		SubCategoryID *uint    `json:"sub_category_id"` // ← add
		Images        []string `json:"images"`
		Location      string   `json:"location"`
		Latitude      *float64 `json:"latitude"`
		Longitude     *float64 `json:"longitude"`
		Condition     string   `json:"condition"`
		Status        string   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := pc.svc.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Post not found"})
		return
	}

	if req.Images != nil {
		imageLimit, err := pc.getImageLimit(existing.UserID)
		if err == nil && len(req.Images) > imageLimit {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":    "image limit exceeded",
				"limit":    imageLimit,
				"uploaded": len(req.Images),
			})
			return
		}
	}

	changes := map[string]interface{}{}
	if req.Title         != "" { changes["title"]           = req.Title }
	if req.Description   != "" { changes["description"]     = req.Description }
	if req.Price         >  0  { changes["price"]           = req.Price }
	if req.CategoryID    >  0  { changes["category_id"]     = req.CategoryID }
	if req.Location      != "" { changes["location"]        = req.Location }
	if req.Latitude      != nil { changes["latitude"]       = *req.Latitude }
	if req.Longitude     != nil { changes["longitude"]      = *req.Longitude }
	if req.Condition     != "" { changes["condition"]       = req.Condition }
	if req.Status        != "" { changes["status"]          = req.Status }
	if req.Images        != nil { changes["images"]         = encodeImages(req.Images) }

	// ── SubCategoryID — allow setting to null or a value ─────────────────────
	if req.SubCategoryID != nil {
		changes["sub_category_id"] = *req.SubCategoryID
	} else {
		// Explicitly null out if not sent — set to nil
		changes["sub_category_id"] = nil
	}

	post, err := pc.svc.AdminUpdate(id, changes)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, post)
}

func (pc *PostController) AdminUpdateStatus(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}
	var body struct {
		Status models.PostStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := pc.svc.UpdateStatus(id, body.Status); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Post status updated", "status": body.Status})
}

func (pc *PostController) AdminDelete(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}
	if err := pc.svc.Delete(id, 0); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Post deleted"})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// getImageLimit fetches the image_limit for a user (defaults to 5).
func (pc *PostController) getImageLimit(userID uint) (int, error) {
	var user models.User
	if err := pc.svc.DB().Select("image_limit").First(&user, userID).Error; err != nil {
		return 0, err
	}
	if user.ImageLimit <= 0 {
		return 5, nil // safe default
	}
	return user.ImageLimit, nil
}

// encodeImages converts a string slice to a JSON array string for DB storage.
func encodeImages(images []string) string {
	if len(images) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(images)
	return string(b)
}

func (pc *PostController) ToggleLike(c *gin.Context) {
	id, ok := postIDParam(c)
	if !ok {
		return
	}
	userID := c.GetUint(middleware.CtxUserID)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Login required to like posts"})
		return
	}

	liked, count, err := pc.svc.ToggleLike(id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"liked":      liked,
		"like_count": count,
	})
}
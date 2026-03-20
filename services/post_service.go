package services

import (
	"errors"

	"gorm.io/gorm"

	"project-api/models"
)

type PostFilter struct {
	Search        string
	CategoryID    uint
	SubCategoryID uint  // ← add
	Status        string
	UserID        uint
}

type PostService struct {
	db *gorm.DB
}

func NewPostService(db *gorm.DB) *PostService {
	return &PostService{db: db}
}

func (s *PostService) DB() *gorm.DB {
	return s.db
}

func (s *PostService) List(f PostFilter) ([]models.Post, error) {
	q := s.db.
		Preload("User").
		Preload("Category").
		Preload("SubCategory") // ← add

	if f.UserID != 0 {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.CategoryID != 0 {
		q = q.Where("category_id = ?", f.CategoryID)
	}
	if f.SubCategoryID != 0 { // ← add
		q = q.Where("sub_category_id = ?", f.SubCategoryID)
	}
	if f.Search != "" {
		like := "%" + f.Search + "%"
		q = q.Where("title LIKE ? OR description LIKE ?", like, like)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	} else if f.UserID == 0 {
		q = q.Where("status = ?", models.StatusActive)
	}
	var posts []models.Post
	return posts, q.Order("created_at DESC").Find(&posts).Error
}

func (s *PostService) GetByID(id uint) (*models.Post, error) {
	var post models.Post
	err := s.db.
		Preload("User").
		Preload("Category").
		Preload("SubCategory"). // ← add
		First(&post, id).Error
	return &post, err
}

func (s *PostService) Create(post *models.Post) error {
	var owner models.User
	if err := s.db.First(&owner, post.UserID).Error; err != nil {
		return errors.New("user not found")
	}
	var count int64
	s.db.Model(&models.Post{}).Where("user_id = ?", post.UserID).Count(&count)
	if int(count) >= owner.PostLimit {
		return errors.New("post limit reached — contact an admin to increase your quota")
	}
	post.Status = models.StatusActive
	if err := s.db.Create(post).Error; err != nil {
		return err
	}
	return s.db.
		Preload("Category").
		Preload("SubCategory"). // ← add
		Preload("User").
		First(post, post.ID).Error
}

func (s *PostService) Update(id, ownerID uint, changes map[string]interface{}) (*models.Post, error) {
	var post models.Post
	if err := s.db.Where("id = ? AND user_id = ?", id, ownerID).First(&post).Error; err != nil {
		return nil, errors.New("post not found or access denied")
	}
	delete(changes, "user_id")
	delete(changes, "id")
	if err := s.db.Model(&post).Updates(changes).Error; err != nil {
		return nil, err
	}
	return &post, s.db.
		Preload("Category").
		Preload("SubCategory"). // ← add
		Preload("User").
		First(&post, post.ID).Error
}

func (s *PostService) Delete(id, ownerID uint) error {
	q := s.db.Where("id = ?", id)
	if ownerID != 0 {
		q = q.Where("user_id = ?", ownerID)
	}
	result := q.Delete(&models.Post{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("post not found or access denied")
	}
	return nil
}

func (s *PostService) UpdateStatus(id uint, status models.PostStatus) error {
	result := s.db.Model(&models.Post{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("post not found")
	}
	return nil
}

func (s *PostService) CountByUser(userID uint) int64 {
	var count int64
	s.db.Model(&models.Post{}).Where("user_id = ?", userID).Count(&count)
	return count
}

// AdminCreate creates a post on behalf of any user, bypassing post-limit check.
func (s *PostService) AdminCreate(post *models.Post) error {
	var owner models.User
	if err := s.db.First(&owner, post.UserID).Error; err != nil {
		return errors.New("target user not found")
	}
	if post.Status == "" {
		post.Status = models.StatusActive
	}
	if err := s.db.Create(post).Error; err != nil {
		return err
	}
	return s.db.
		Preload("Category").
		Preload("SubCategory"). // ← add
		Preload("User").
		First(post, post.ID).Error
}

// AdminUpdate updates any post field — admin only, no ownership check.
func (s *PostService) AdminUpdate(id uint, changes map[string]interface{}) (*models.Post, error) {
	var post models.Post
	if err := s.db.First(&post, id).Error; err != nil {
		return nil, errors.New("post not found")
	}
	delete(changes, "id")
	delete(changes, "user_id")
	if err := s.db.Model(&post).Updates(changes).Error; err != nil {
		return nil, err
	}
	return &post, s.db.
		Preload("Category").
		Preload("SubCategory"). // ← add
		Preload("User").
		First(&post, post.ID).Error
}

// IncrementView adds 1 to the post's view counter.
func (s *PostService) IncrementView(id uint) {
	s.db.Model(&models.Post{}).Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1"))
}

// ToggleLike adds or removes a like for (userID, postID).
func (s *PostService) ToggleLike(postID, userID uint) (bool, int, error) {
	var existing models.PostLike

	err := s.db.Unscoped().
		Where("post_id = ? AND user_id = ?", postID, userID).
		First(&existing).Error

	if err == nil {
		if delErr := s.db.Unscoped().Delete(&existing).Error; delErr != nil {
			return false, 0, delErr
		}
		var count int64
		s.db.Model(&models.PostLike{}).Where("post_id = ?", postID).Count(&count)
		return false, int(count), nil
	}

	like := models.PostLike{PostID: postID, UserID: userID}
	if createErr := s.db.Create(&like).Error; createErr != nil {
		return false, 0, createErr
	}

	var count int64
	s.db.Model(&models.PostLike{}).Where("post_id = ?", postID).Count(&count)
	return true, int(count), nil
}

// AttachLikeMeta populates LikeCount and IsLiked on a slice of posts.
func (s *PostService) AttachLikeMeta(posts []models.Post, userID uint) {
	if len(posts) == 0 {
		return
	}

	ids := make([]uint, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}

	type countRow struct {
		PostID uint
		Total  int
	}
	var counts []countRow
	s.db.Model(&models.PostLike{}).
		Select("post_id, COUNT(*) as total").
		Where("post_id IN ?", ids).
		Group("post_id").
		Scan(&counts)

	countMap := make(map[uint]int, len(counts))
	for _, r := range counts {
		countMap[r.PostID] = r.Total
	}

	likedSet := make(map[uint]bool)
	if userID > 0 {
		var liked []models.PostLike
		s.db.Where("post_id IN ? AND user_id = ?", ids, userID).Find(&liked)
		for _, l := range liked {
			likedSet[l.PostID] = true
		}
	}

	for i := range posts {
		posts[i].LikeCount = countMap[posts[i].ID]
		posts[i].IsLiked   = likedSet[posts[i].ID]
	}
}
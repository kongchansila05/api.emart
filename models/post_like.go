package models

import "gorm.io/gorm"

// PostLike records that a user liked a post.
// Unique constraint prevents double-liking.
type PostLike struct {
	gorm.Model
	PostID uint `json:"post_id" gorm:"uniqueIndex:idx_post_user_like;not null"`
	UserID uint `json:"user_id" gorm:"uniqueIndex:idx_post_user_like;not null"`
}

func (PostLike) TableName() string { return "post_likes" }
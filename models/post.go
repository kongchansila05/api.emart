package models

import "gorm.io/gorm"

type PostStatus string

const (
	StatusActive  PostStatus = "active"
	StatusSold    PostStatus = "sold"
	StatusPending PostStatus = "pending"
)

type Post struct {
	gorm.Model
	Title       string     `json:"title"       gorm:"not null"`
	Description string     `json:"description"`
	Price       float64    `json:"price"       gorm:"not null"`
	Images      string     `json:"images"`
	Status      PostStatus `json:"status"      gorm:"default:'active'"`
	Location    string     `json:"location"`
	Latitude    float64    `json:"latitude"    gorm:"default:0"`   // ← add
	Longitude   float64    `json:"longitude"   gorm:"default:0"`   // ← add
	Condition   string     `json:"condition"`
	ViewCount   int        `json:"view_count"  gorm:"default:0"`

	UserID     uint     `json:"user_id"`
	User       User     `json:"user"     gorm:"foreignKey:UserID"`
	CategoryID uint     `json:"category_id"`
	Category   Category `json:"category" gorm:"foreignKey:CategoryID"`
	SubCategoryID *uint       `json:"sub_category_id"`                          // ← add (nullable)
	SubCategory   SubCategory `json:"sub_category" gorm:"foreignKey:SubCategoryID"` // ← add

	// Virtual — populated manually per request
	LikeCount int  `json:"like_count" gorm:"-"`
	IsLiked   bool `json:"is_liked"   gorm:"-"`
}

func (Post) TableName() string { return "posts" }
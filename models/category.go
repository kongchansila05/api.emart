package models

import "gorm.io/gorm"

// Category groups marketplace posts into logical buckets.
type Category struct {
	gorm.Model
	Name          string        `json:"name" gorm:"type:varchar(150);uniqueIndex;not null"`
	Description   string        `json:"description" gorm:"type:varchar(255)"`
	Image         string        `json:"image" gorm:"type:varchar(255)"` // public R2 URL
	IsActive      bool          `json:"is_active" gorm:"default:true"`
	PostCount     int           `json:"post_count,omitempty" gorm:"-"`
	SubCategories []SubCategory `json:"sub_categories" gorm:"foreignKey:CategoryID"`
}

func (Category) TableName() string { return "categories" }

// SubCategory groups posts under a parent Category
type SubCategory struct {
	gorm.Model
	CategoryID  uint   `json:"category_id" gorm:"index;not null"`
	Name        string `json:"name" gorm:"type:varchar(150);not null"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	Image       string `json:"image" gorm:"type:varchar(255)"`
	IsActive    bool   `json:"is_active" gorm:"default:true"`
	PostCount   int    `json:"post_count" gorm:"-"`
}

func (SubCategory) TableName() string { return "sub_categories" }
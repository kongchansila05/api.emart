package models

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Name       string `json:"name"        gorm:"type:varchar(100);not null"`
	Email      string `json:"email"       gorm:"type:varchar(150);uniqueIndex;not null"`
	Password   string `json:"-"           gorm:"type:varchar(255);not null"`
	Phone      string `json:"phone"       gorm:"type:varchar(50)"`
	Avatar     string `json:"avatar"      gorm:"type:varchar(255)"`
	RoleID     uint   `json:"role_id"`
	Role       Role   `json:"role"        gorm:"foreignKey:RoleID"`
	PostLimit  int    `json:"post_limit"  gorm:"default:10"`
	ImageLimit int    `json:"image_limit" gorm:"default:5"` // max images per post
	IsActive   bool   `json:"is_active"   gorm:"default:true"`

	// Virtual — populated manually, not stored in DB.
	PostCount int `json:"post_count,omitempty" gorm:"-"`
}

func (User) TableName() string { return "users" }
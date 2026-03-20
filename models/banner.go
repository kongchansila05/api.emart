package models

import (
	"time"
	"gorm.io/gorm"
)

type BannerPosition string

const (
	BannerPositionTop     BannerPosition = "top"
	BannerPositionMiddle  BannerPosition = "middle"
	BannerPositionBottom  BannerPosition = "bottom"
	BannerPositionSidebar BannerPosition = "sidebar"
)

type Banner struct {
	gorm.Model
	Title     string         `json:"title"      gorm:"not null"`
	Image     string         `json:"image"      gorm:"not null"`
	LinkURL   string         `json:"link_url"`
	Position  BannerPosition `json:"position"   gorm:"default:'top'"`
	SortOrder int            `json:"sort_order" gorm:"default:0"`
	IsActive  bool           `json:"is_active"  gorm:"default:true"`
	StartsAt  *time.Time     `json:"starts_at"`
	EndsAt    *time.Time     `json:"ends_at"`
}

func (Banner) TableName() string { return "banners" }
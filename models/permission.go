package models

import "gorm.io/gorm"

// Permission represents a single granular access key.
type Permission struct {
	gorm.Model
	Name        string `json:"name" gorm:"type:varchar(100);uniqueIndex;not null"`
	Description string `json:"description" gorm:"type:varchar(255)"`

	Roles []Role `json:"-" gorm:"many2many:role_permissions;"`
}
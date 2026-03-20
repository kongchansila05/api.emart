package models

import "gorm.io/gorm"

// Role represents a named set of permissions assigned to users.
type Role struct {
	gorm.Model
	Name        string       `json:"name" gorm:"type:varchar(100);uniqueIndex;not null"`
	Description string       `json:"description" gorm:"type:varchar(255)"`
	Permissions []Permission `json:"permissions" gorm:"many2many:role_permissions;"`
}
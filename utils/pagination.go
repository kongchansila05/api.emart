package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	DefaultLimit = 10
	MaxLimit     = 100 // prevent ?limit=999999 abuse
)

// PageMeta holds pagination metadata returned in every paginated response.
type PageMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// PageResult is the generic envelope returned to the client.
type PageResult[T any] struct {
	Data []T      `json:"data"`
	Meta PageMeta `json:"meta"`
}

// PageParams extracts and validates page/limit from query string.
func PageParams(c *gin.Context, defaultLimit int) (page, limit, offset int) {
	page  = 1
	limit = defaultLimit

	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}

	// guard against abuse
	if limit > MaxLimit {
		limit = MaxLimit
	}

	offset = (page - 1) * limit
	return
}

// Paginate applies COUNT + LIMIT/OFFSET to any model.
// Pass a pre-scoped *gorm.DB (with Where, Joins, etc already applied).
//
// Example:
//
//	scope := db.Model(&models.Category{}).Where("is_active = ?", true)
//	result, err := utils.Paginate[models.Category](scope, c, 10)
func Paginate[T any](scope *gorm.DB, c *gin.Context, defaultLimit int) (PageResult[T], error) {
	page, limit, offset := PageParams(c, defaultLimit)

	// COUNT on a separate session so LIMIT/OFFSET don't interfere
	var total int64
	if err := scope.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return PageResult[T]{}, err
	}

	var items []T
	if err := scope.Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return PageResult[T]{}, err
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	return PageResult[T]{
		Data: items,
		Meta: PageMeta{
			Page:       page,
			Limit:      limit,
			TotalItems: total,
			TotalPages: totalPages,
			HasNext:    page < totalPages,
			HasPrev:    page > 1,
		},
	}, nil
}
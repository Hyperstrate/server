// Package pagination provides shared types and helpers for paginated list responses.
// All list endpoints accept ?page=1&perPage=30 and return a Paginated envelope.
package pagination

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	DefaultPage    = 1
	DefaultPerPage = 30
	MinPage        = 1
	MinPerPage     = 1
	MaxPerPage     = 500
)

// Slice holds the parsed page / perPage query parameters.
type Slice struct {
	Page    int
	PerPage int
}

// Offset returns the zero-based row offset for a SQL OFFSET clause.
func (s Slice) Offset() int {
	if s.Page < 1 {
		return 0
	}
	return (s.Page - 1) * s.PerPage
}

// PaginatedMeta carries pagination statistics returned to the caller.
type PaginatedMeta struct {
	Total   int64 `json:"total"   validate:"required"`
	Count   int   `json:"count"   validate:"required"`
	PerPage int   `json:"perPage" validate:"required"`
	Pages   int   `json:"pages"   validate:"required"`
	Page    int   `json:"page"    validate:"required"`
}

// Paginated is the standard list envelope.
type Paginated[T any] struct {
	Items []T           `json:"items" validate:"required"`
	Meta  PaginatedMeta `json:"meta" validate:"required"`
}

// New builds a Paginated from a pre-fetched page of items and total count.
func New[T any](items []T, total int64, slice Slice) Paginated[T] {
	if items == nil {
		items = []T{}
	}
	pages := 1
	if slice.PerPage > 0 {
		pages = int(math.Ceil(float64(total) / float64(slice.PerPage)))
	}
	if pages < 1 {
		pages = 1
	}
	return Paginated[T]{
		Items: items,
		Meta: PaginatedMeta{
			Total:   total,
			Count:   len(items),
			PerPage: slice.PerPage,
			Pages:   pages,
			Page:    slice.Page,
		},
	}
}

// ParseSlice reads page and perPage from Gin query params, applying defaults and clamping.
func ParseSlice(c *gin.Context) Slice {
	page := queryInt(c, "page", DefaultPage)
	perPage := queryInt(c, "perPage", DefaultPerPage)

	if page < MinPage {
		page = MinPage
	}
	if perPage < MinPerPage {
		perPage = MinPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	return Slice{Page: page, PerPage: perPage}
}

func queryInt(c *gin.Context, key string, fallback int) int {
	if raw := c.Query(key); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			return v
		}
	}
	return fallback
}

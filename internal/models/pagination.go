package models

import "math"

// SortOrder is the direction of an ORDER BY clause.
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// Valid reports whether o is a recognised sort direction.
func (o SortOrder) Valid() bool { return o == SortAsc || o == SortDesc }

func (o SortOrder) String() string { return string(o) }

// Pagination defaults and guard rails. MaxLimit prevents a client from asking
// for the whole table in one request.
const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// PageParams describes a page of results plus the optional free-text search.
type PageParams struct {
	Page   int       `json:"page"`
	Limit  int       `json:"limit"`
	Sort   string    `json:"sort"`
	Order  SortOrder `json:"order"`
	Search string    `json:"search,omitempty"`
}

// Normalize clamps user-supplied values into the supported range.
func (p PageParams) Normalize(defaultSort string) PageParams {
	if p.Page < 1 {
		p.Page = DefaultPage
	}
	if p.Limit < 1 {
		p.Limit = DefaultLimit
	}
	if p.Limit > MaxLimit {
		p.Limit = MaxLimit
	}
	if p.Sort == "" {
		p.Sort = defaultSort
	}
	if !p.Order.Valid() {
		p.Order = SortDesc
	}
	return p
}

// Offset is the SQL OFFSET for this page.
func (p PageParams) Offset() int {
	if p.Page < 1 {
		return 0
	}
	return (p.Page - 1) * p.Limit
}

// PageMeta is the pagination envelope returned alongside a list payload.
type PageMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// NewPageMeta derives the response metadata from the request and total count.
func NewPageMeta(p PageParams, total int64) PageMeta {
	limit := p.Limit
	if limit < 1 {
		limit = DefaultLimit
	}
	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	if totalPages < 1 {
		totalPages = 1
	}
	return PageMeta{
		Page:       p.Page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    p.Page < totalPages,
		HasPrev:    p.Page > 1,
	}
}

// Page bundles a slice of results with its pagination metadata.
type Page[T any] struct {
	Items []T      `json:"items"`
	Meta  PageMeta `json:"meta"`
}

package kibotos

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
)

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T {
	return &v
}

// Nullable represents an optional JSON field with three states:
// unset (omitted during serialization), explicit null, or a concrete value.
type Nullable[T any] struct {
	Set   bool
	Valid bool
	Value T
}

// Some returns a Nullable set to a non-null value.
func Some[T any](v T) Nullable[T] {
	return Nullable[T]{Set: true, Valid: true, Value: v}
}

// Null returns a Nullable explicitly set to JSON null.
func Null[T any]() Nullable[T] {
	return Nullable[T]{Set: true, Valid: false}
}

// MarshalJSON encodes a set Nullable as its value or null.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if !n.Set || !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}

// Page represents a standard cursor-paginated response envelope.
type Page[T any] struct {
	Data          []T     `json:"data"`
	NextPageToken *string `json:"next_page_token"`
}

// HasMore reports whether another page of results is available.
func (p *Page[T]) HasMore() bool {
	return p != nil && p.NextPageToken != nil && *p.NextPageToken != ""
}

// Token returns the next page token, or an empty string if there is none.
func (p *Page[T]) Token() string {
	if !p.HasMore() {
		return ""
	}
	return *p.NextPageToken
}

// MultiZonePage represents a paginated response envelope aggregated across zones.
type MultiZonePage[T any] struct {
	Data             []T      `json:"data"`
	UnavailableZones []string `json:"unavailable_zones"`
	NextPageToken    *string  `json:"next_page_token"`
}

// HasMore reports whether another page of results is available.
func (p *MultiZonePage[T]) HasMore() bool {
	return p != nil && p.NextPageToken != nil && *p.NextPageToken != ""
}

// Token returns the next page token, or an empty string if there is none.
func (p *MultiZonePage[T]) Token() string {
	if !p.HasMore() {
		return ""
	}
	return *p.NextPageToken
}

// FailureInfo describes a resource failure returned by the Kibotos API.
type FailureInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func addPaginationParams(q url.Values, limit int, pageToken string) {
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
}

func addLabelParams(q url.Values, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		q.Set("label."+k, labels[k])
	}
}

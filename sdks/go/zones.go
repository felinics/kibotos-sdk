package kibotos

import (
	"context"
	"iter"
	"net/http"
	"net/url"
)

// ZoneStatus represents the operational status of a zone.
type ZoneStatus string

const (
	ZoneStatusAvailable ZoneStatus = "available"
	ZoneStatusDraining  ZoneStatus = "draining"
)

// Zone represents a compute zone available in Kibotos.
type Zone struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	Description *string    `json:"description"`
	Region      string     `json:"region"`
	Country     *string    `json:"country"`
	Status      ZoneStatus `json:"status"`
	Default     bool       `json:"default"`
}

// ZoneListParams specifies optional pagination parameters for listing zones.
type ZoneListParams struct {
	Limit     int
	PageToken string
}

// ZonesService handles communication with the zones endpoints.
type ZonesService struct {
	client *Client
}

// List returns a page of zones.
func (s *ZonesService) List(ctx context.Context, params *ZoneListParams, opts ...RequestOption) (*Page[Zone], error) {
	q := url.Values{}
	if params != nil {
		addPaginationParams(q, params.Limit, params.PageToken)
	}
	var page Page[Zone]
	if err := s.client.do(ctx, http.MethodGet, "/zones", q, nil, &page, opts...); err != nil {
		return nil, err
	}
	return &page, nil
}

// Iter returns an iterator over all zones across pages.
func (s *ZonesService) Iter(ctx context.Context, params *ZoneListParams, opts ...RequestOption) iter.Seq2[Zone, error] {
	return func(yield func(Zone, error) bool) {
		var p ZoneListParams
		if params != nil {
			p = *params
		}
		for {
			page, err := s.List(ctx, &p, opts...)
			if err != nil {
				yield(Zone{}, err)
				return
			}
			for _, item := range page.Data {
				if !yield(item, nil) {
					return
				}
			}
			next := page.Token()
			if next == "" || next == p.PageToken {
				return
			}
			p.PageToken = next
		}
	}
}

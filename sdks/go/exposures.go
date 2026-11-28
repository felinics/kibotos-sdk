package kibotos

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ExposureStatus represents the lifecycle state of a port exposure.
type ExposureStatus string

const (
	ExposureStatusActive  ExposureStatus = "active"
	ExposureStatusRevoked ExposureStatus = "revoked"
)

// Exposure represents a temporary HTTPS exposure for a workspace port.
type Exposure struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Port        int            `json:"port"`
	URL         string         `json:"url"`
	Status      ExposureStatus `json:"status"`
	ExpiresAt   time.Time      `json:"expires_at"`
	RevokedAt   *time.Time     `json:"revoked_at"`
	CreatedAt   time.Time      `json:"created_at"`
}

// CreateExposureParams defines parameters for POST /v1/workspaces/{id}/exposures.
type CreateExposureParams struct {
	Port       int `json:"port"`
	TTLSeconds int `json:"ttl_seconds,omitempty"`
}

// ExposureListParams defines optional filters and pagination for GET /v1/workspaces/{id}/exposures.
type ExposureListParams struct {
	IncludeRevoked bool
	Limit          int
	PageToken      string
}

// ExposuresService handles communication with workspace exposure endpoints.
type ExposuresService struct {
	client *Client
}

// Create creates a new HTTPS exposure for a workspace port (POST /v1/workspaces/{id}/exposures).
func (s *ExposuresService) Create(ctx context.Context, workspaceID string, params CreateExposureParams, opts ...RequestOption) (*Exposure, error) {
	if workspaceID == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	if params.Port <= 0 || params.Port > 65535 {
		return nil, errors.New("kibotos: port must be between 1 and 65535")
	}
	var exp Exposure
	path := "/workspaces/" + url.PathEscape(workspaceID) + "/exposures"
	if err := s.client.do(ctx, http.MethodPost, path, nil, params, &exp, opts...); err != nil {
		return nil, err
	}
	return &exp, nil
}

// List returns a page of exposures for a workspace (GET /v1/workspaces/{id}/exposures).
func (s *ExposuresService) List(ctx context.Context, workspaceID string, params *ExposureListParams, opts ...RequestOption) (*Page[Exposure], error) {
	if workspaceID == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	q := url.Values{}
	if params != nil {
		if params.IncludeRevoked {
			q.Set("include_revoked", strconv.FormatBool(true))
		}
		addPaginationParams(q, params.Limit, params.PageToken)
	}
	var page Page[Exposure]
	path := "/workspaces/" + url.PathEscape(workspaceID) + "/exposures"
	if err := s.client.do(ctx, http.MethodGet, path, q, nil, &page, opts...); err != nil {
		return nil, err
	}
	return &page, nil
}

// Iter returns an iterator over all exposures for a workspace across pages.
func (s *ExposuresService) Iter(ctx context.Context, workspaceID string, params *ExposureListParams, opts ...RequestOption) iter.Seq2[Exposure, error] {
	return func(yield func(Exposure, error) bool) {
		var p ExposureListParams
		if params != nil {
			p = *params
		}
		for {
			page, err := s.List(ctx, workspaceID, &p, opts...)
			if err != nil {
				yield(Exposure{}, err)
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

// Get retrieves a single exposure for a workspace (GET /v1/workspaces/{id}/exposures/{exposure_id}).
func (s *ExposuresService) Get(ctx context.Context, workspaceID, exposureID string, opts ...RequestOption) (*Exposure, error) {
	if workspaceID == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	if exposureID == "" {
		return nil, errors.New("kibotos: exposure id is required")
	}
	var exp Exposure
	path := "/workspaces/" + url.PathEscape(workspaceID) + "/exposures/" + url.PathEscape(exposureID)
	if err := s.client.do(ctx, http.MethodGet, path, nil, nil, &exp, opts...); err != nil {
		return nil, err
	}
	return &exp, nil
}

// Revoke revokes an active workspace exposure (DELETE /v1/workspaces/{id}/exposures/{exposure_id}).
func (s *ExposuresService) Revoke(ctx context.Context, workspaceID, exposureID string, opts ...RequestOption) (*Exposure, error) {
	if workspaceID == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	if exposureID == "" {
		return nil, errors.New("kibotos: exposure id is required")
	}
	var exp Exposure
	path := "/workspaces/" + url.PathEscape(workspaceID) + "/exposures/" + url.PathEscape(exposureID)
	if err := s.client.do(ctx, http.MethodDelete, path, nil, nil, &exp, opts...); err != nil {
		return nil, err
	}
	return &exp, nil
}

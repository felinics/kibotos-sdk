package kibotos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

type Pack struct {
	ID                   string    `json:"id"`
	DisplayName          string    `json:"display_name"`
	Description          string    `json:"description"`
	DefaultRevision      string    `json:"default_revision"`
	Architecture         string    `json:"architecture"`
	Version              string    `json:"version"`
	GuestRequirements    []string  `json:"guest_requirements"`
	ProvidedCapabilities []string  `json:"provided_capabilities"`
	PublishedAt          time.Time `json:"published_at"`
	Zones                []string  `json:"zones"`
}

type PackRevision struct {
	ID                   string    `json:"id"`
	PackID               string    `json:"pack_id"`
	Architecture         string    `json:"architecture"`
	Version              string    `json:"version"`
	GuestRequirements    []string  `json:"guest_requirements"`
	ProvidedCapabilities []string  `json:"provided_capabilities"`
	PublishedAt          time.Time `json:"published_at"`
	Zones                []string  `json:"zones"`
}

type PackListParams struct {
	Zone      string
	Limit     int
	PageToken string
}

// PacksService reads the platform Pack catalog.
type PacksService struct{ client *Client }

func (s *PacksService) List(ctx context.Context, params *PackListParams, opts ...RequestOption) (*MultiZonePage[Pack], error) {
	query := url.Values{}
	if params != nil {
		addPaginationParams(query, params.Limit, params.PageToken)
		if params.Zone != "" {
			query.Set("zone", params.Zone)
		}
	}
	return requestJSON[MultiZonePage[Pack]](ctx, s.client, http.MethodGet, "/packs", query, nil, opts)
}

func (s *PacksService) Get(ctx context.Context, id string, opts ...RequestOption) (*Pack, error) {
	if id == "" {
		return nil, errors.New("kibotos: pack id is required")
	}
	return requestJSON[Pack](ctx, s.client, http.MethodGet, "/packs/"+url.PathEscape(id), nil, nil, opts)
}

func (s *PacksService) GetRevision(ctx context.Context, packID, revisionID string, opts ...RequestOption) (*PackRevision, error) {
	if packID == "" || revisionID == "" {
		return nil, errors.New("kibotos: pack id and revision id are required")
	}
	path := "/packs/" + url.PathEscape(packID) + "/revisions/" + url.PathEscape(revisionID)
	return requestJSON[PackRevision](ctx, s.client, http.MethodGet, path, nil, nil, opts)
}

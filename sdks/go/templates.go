package kibotos

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/url"
	"time"
)

// TemplateOwner represents the ownership scope of a template.
type TemplateOwner string

const (
	TemplateOwnerPlatform     TemplateOwner = "platform"
	TemplateOwnerOrganization TemplateOwner = "organization"
)

// TemplateRevisionSummary is a compact reference to a template revision.
type TemplateRevisionSummary struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Template represents a workspace template.
type Template struct {
	ID              string                   `json:"id"`
	Owner           TemplateOwner            `json:"owner"`
	DisplayName     string                   `json:"display_name"`
	Description     string                   `json:"description"`
	DefaultRevision string                   `json:"default_revision"`
	LatestRevision  *TemplateRevisionSummary `json:"latest_revision"`
	Zones           []string                 `json:"zones"`
	CreatedAt       *time.Time               `json:"created_at"`
	UpdatedAt       *time.Time               `json:"updated_at"`
}

// TemplateRevision represents a specific immutable revision of a template.
type TemplateRevision struct {
	ID         string         `json:"id"`
	TemplateID string         `json:"template_id"`
	Status     string         `json:"status"`
	Failure    *FailureInfo   `json:"failure"`
	Source     map[string]any `json:"source"`
	Process    map[string]any `json:"process"`
	SizeMiB    *int64         `json:"size_mib"`
	Zones      []string       `json:"zones"`
	CreatedAt  *time.Time     `json:"created_at"`
	ReadyAt    *time.Time     `json:"ready_at"`
}

// TemplateListParams specifies optional filters and pagination for listing templates.
type TemplateListParams struct {
	Owner     TemplateOwner
	Zone      string
	Limit     int
	PageToken string
}

// TemplateRevisionListParams specifies optional pagination for listing template revisions.
type TemplateRevisionListParams struct {
	Limit     int
	PageToken string
}

// TemplatesService handles communication with the templates endpoints.
type TemplatesService struct {
	client *Client
}

// List returns a page of templates across zones.
func (s *TemplatesService) List(ctx context.Context, params *TemplateListParams, opts ...RequestOption) (*MultiZonePage[Template], error) {
	q := url.Values{}
	if params != nil {
		if params.Owner != "" {
			q.Set("owner", string(params.Owner))
		}
		if params.Zone != "" {
			q.Set("zone", params.Zone)
		}
		addPaginationParams(q, params.Limit, params.PageToken)
	}
	var page MultiZonePage[Template]
	if err := s.client.do(ctx, http.MethodGet, "/templates", q, nil, &page, opts...); err != nil {
		return nil, err
	}
	return &page, nil
}

// Iter returns an iterator over all matching templates across pages.
func (s *TemplatesService) Iter(ctx context.Context, params *TemplateListParams, opts ...RequestOption) iter.Seq2[Template, error] {
	return func(yield func(Template, error) bool) {
		var p TemplateListParams
		if params != nil {
			p = *params
		}
		for {
			page, err := s.List(ctx, &p, opts...)
			if err != nil {
				yield(Template{}, err)
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

// Get retrieves a single template by ID.
func (s *TemplatesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Template, error) {
	if id == "" {
		return nil, errors.New("kibotos: template id is required")
	}
	var tpl Template
	if err := s.client.do(ctx, http.MethodGet, "/templates/"+url.PathEscape(id), nil, nil, &tpl, opts...); err != nil {
		return nil, err
	}
	return &tpl, nil
}

// ListRevisions returns a page of revisions for a template.
func (s *TemplatesService) ListRevisions(ctx context.Context, templateID string, params *TemplateRevisionListParams, opts ...RequestOption) (*Page[TemplateRevision], error) {
	if templateID == "" {
		return nil, errors.New("kibotos: template id is required")
	}
	q := url.Values{}
	if params != nil {
		addPaginationParams(q, params.Limit, params.PageToken)
	}
	var page Page[TemplateRevision]
	path := "/templates/" + url.PathEscape(templateID) + "/revisions"
	if err := s.client.do(ctx, http.MethodGet, path, q, nil, &page, opts...); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetRevision retrieves a specific revision of a template.
func (s *TemplatesService) GetRevision(ctx context.Context, templateID, revisionID string, opts ...RequestOption) (*TemplateRevision, error) {
	if templateID == "" {
		return nil, errors.New("kibotos: template id is required")
	}
	if revisionID == "" {
		return nil, errors.New("kibotos: revision id is required")
	}
	var rev TemplateRevision
	path := "/templates/" + url.PathEscape(templateID) + "/revisions/" + url.PathEscape(revisionID)
	if err := s.client.do(ctx, http.MethodGet, path, nil, nil, &rev, opts...); err != nil {
		return nil, err
	}
	return &rev, nil
}

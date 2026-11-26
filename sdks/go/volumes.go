package kibotos

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// VolumeStatus represents the lifecycle state of a volume.
// Values map 1:1 to native Kibotos VolumeState in lowercase:
// Volume.WorkspaceID expresses workspace binding, "available" means ready and not
// currently mounted to a running execution, "attached" means mounted to a running
// workspace, and "lost" means the underlying storage pool was lost.
type VolumeStatus string

const (
	VolumeStatusCreating  VolumeStatus = "creating"
	VolumeStatusAvailable VolumeStatus = "available"
	VolumeStatusAttached  VolumeStatus = "attached"
	VolumeStatusResizing  VolumeStatus = "resizing"
	VolumeStatusDeleting  VolumeStatus = "deleting"
	VolumeStatusFailed    VolumeStatus = "failed"
	VolumeStatusLost      VolumeStatus = "lost"
	VolumeStatusUnknown   VolumeStatus = "unknown"
)

// Volume represents a persistent storage volume in Kibotos.
type Volume struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Zone        string            `json:"zone"`
	SizeGiB     int               `json:"size_gib"`
	Status      VolumeStatus      `json:"status"`
	Failure     *FailureInfo      `json:"failure"`
	WorkspaceID *string           `json:"workspace_id"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// CreateVolumeParams defines parameters for POST /v1/volumes.
// SizeGiB is required; all other fields are optional.
type CreateVolumeParams struct {
	Name    string            `json:"name,omitempty"`
	Zone    string            `json:"zone,omitempty"`
	SizeGiB int               `json:"size_gib"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// VolumeListParams defines optional filters and pagination for GET /v1/volumes.
type VolumeListParams struct {
	Zone      string
	Attached  *bool
	Labels    map[string]string
	Limit     int
	PageToken string
}

// UpdateVolumeParams defines parameters for PATCH /v1/volumes/{id}.
type UpdateVolumeParams struct {
	Name   Nullable[string]
	Labels map[string]*string
}

// MarshalJSON encodes only the fields explicitly configured in UpdateVolumeParams.
func (p UpdateVolumeParams) MarshalJSON() ([]byte, error) {
	body := make(map[string]any, 2)
	if p.Name.Set {
		if p.Name.Valid {
			body["name"] = p.Name.Value
		} else {
			body["name"] = nil
		}
	}
	if p.Labels != nil {
		body["labels"] = p.Labels
	}
	return json.Marshal(body)
}

// ResizeVolumeParams defines parameters for POST /v1/volumes/{id}/resize.
type ResizeVolumeParams struct {
	SizeGiB int `json:"size_gib"`
}

// WaitVolumeParams defines parameters for GET /v1/volumes/{id}/wait.
type WaitVolumeParams struct {
	Statuses []VolumeStatus
	Timeout  time.Duration
}

// VolumesService handles communication with the volumes endpoints.
type VolumesService struct {
	client *Client
}

// Create provisions a new volume (POST /v1/volumes).
func (s *VolumesService) Create(ctx context.Context, params CreateVolumeParams, opts ...RequestOption) (*Volume, error) {
	if params.SizeGiB <= 0 {
		return nil, errors.New("kibotos: size_gib must be greater than zero")
	}
	var vol Volume
	if err := s.client.do(ctx, http.MethodPost, "/volumes", nil, params, &vol, opts...); err != nil {
		return nil, err
	}
	return &vol, nil
}

// List returns a page of volumes across zones (GET /v1/volumes).
func (s *VolumesService) List(ctx context.Context, params *VolumeListParams, opts ...RequestOption) (*MultiZonePage[Volume], error) {
	q := url.Values{}
	if params != nil {
		if params.Zone != "" {
			q.Set("zone", params.Zone)
		}
		if params.Attached != nil {
			q.Set("attached", strconv.FormatBool(*params.Attached))
		}
		addLabelParams(q, params.Labels)
		addPaginationParams(q, params.Limit, params.PageToken)
	}
	var page MultiZonePage[Volume]
	if err := s.client.do(ctx, http.MethodGet, "/volumes", q, nil, &page, opts...); err != nil {
		return nil, err
	}
	return &page, nil
}

// Iter returns an iterator over all matching volumes across pages.
func (s *VolumesService) Iter(ctx context.Context, params *VolumeListParams, opts ...RequestOption) iter.Seq2[Volume, error] {
	return func(yield func(Volume, error) bool) {
		var p VolumeListParams
		if params != nil {
			p = *params
		}
		for {
			page, err := s.List(ctx, &p, opts...)
			if err != nil {
				yield(Volume{}, err)
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

// Get retrieves a volume by ID (GET /v1/volumes/{id}).
func (s *VolumesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Volume, error) {
	if id == "" {
		return nil, errors.New("kibotos: volume id is required")
	}
	var vol Volume
	if err := s.client.do(ctx, http.MethodGet, "/volumes/"+url.PathEscape(id), nil, nil, &vol, opts...); err != nil {
		return nil, err
	}
	return &vol, nil
}

// Update modifies a volume's name or labels (PATCH /v1/volumes/{id}).
func (s *VolumesService) Update(ctx context.Context, id string, params UpdateVolumeParams, opts ...RequestOption) (*Volume, error) {
	if id == "" {
		return nil, errors.New("kibotos: volume id is required")
	}
	var vol Volume
	if err := s.client.do(ctx, http.MethodPatch, "/volumes/"+url.PathEscape(id), nil, params, &vol, opts...); err != nil {
		return nil, err
	}
	return &vol, nil
}

// Resize grows a volume to a new size (POST /v1/volumes/{id}/resize).
func (s *VolumesService) Resize(ctx context.Context, id string, params ResizeVolumeParams, opts ...RequestOption) (*Volume, error) {
	if id == "" {
		return nil, errors.New("kibotos: volume id is required")
	}
	if params.SizeGiB <= 0 {
		return nil, errors.New("kibotos: size_gib must be greater than zero")
	}
	var vol Volume
	path := "/volumes/" + url.PathEscape(id) + "/resize"
	if err := s.client.do(ctx, http.MethodPost, path, nil, params, &vol, opts...); err != nil {
		return nil, err
	}
	return &vol, nil
}

// Delete initiates deletion of a volume (DELETE /v1/volumes/{id}).
func (s *VolumesService) Delete(ctx context.Context, id string, opts ...RequestOption) (*Volume, error) {
	if id == "" {
		return nil, errors.New("kibotos: volume id is required")
	}
	var vol Volume
	if err := s.client.do(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(id), nil, nil, &vol, opts...); err != nil {
		return nil, err
	}
	return &vol, nil
}

// Wait blocks until the volume reaches one of params.Statuses, fails with
// unexpected_status, or the timeout / context deadline expires.
func (s *VolumesService) Wait(ctx context.Context, id string, params WaitVolumeParams, opts ...RequestOption) (*Volume, error) {
	if id == "" {
		return nil, errors.New("kibotos: volume id is required")
	}
	if len(params.Statuses) == 0 {
		return nil, errors.New("kibotos: at least one target status is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	waitCtx, deadline, cancel := resolveWaitDeadline(ctx, params.Timeout)
	defer cancel()

	path := "/volumes/" + url.PathEscape(id) + "/wait"
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		chunkSecs := computeWaitChunkSeconds(remaining)

		q := url.Values{}
		for _, st := range params.Statuses {
			q.Add("status", string(st))
		}
		q.Set("timeout", strconv.Itoa(chunkSecs))

		reqOpts := make([]RequestOption, 0, len(opts)+1)
		reqOpts = append(reqOpts, opts...)
		reqOpts = append(reqOpts, withWaitRequestConfig(time.Duration(chunkSecs)*time.Second+15*time.Second))

		var vol Volume
		err := s.client.do(waitCtx, http.MethodGet, path, q, nil, &vol, reqOpts...)
		if err == nil {
			return &vol, nil
		}
		if IsWaitTimeout(err) {
			if waitCtx.Err() != nil || time.Until(deadline) <= 0 {
				return nil, err
			}
			continue
		}
		return nil, err
	}
}

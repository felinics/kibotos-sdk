package kibotos

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// WorkspaceStatus represents the lifecycle state of a workspace.
// Values map 1:1 to native Kibotos WorkspaceState in lowercase.
type WorkspaceStatus string

const (
	WorkspaceStatusCreating  WorkspaceStatus = "creating"
	WorkspaceStatusAvailable WorkspaceStatus = "available"
	WorkspaceStatusStopping  WorkspaceStatus = "stopping"
	WorkspaceStatusStopped   WorkspaceStatus = "stopped"
	WorkspaceStatusStarting  WorkspaceStatus = "starting"
	WorkspaceStatusDeleting  WorkspaceStatus = "deleting"
	WorkspaceStatusFailed    WorkspaceStatus = "failed"
	WorkspaceStatusUnknown   WorkspaceStatus = "unknown"
)

// NetworkMode represents a workspace's outbound network policy.
type NetworkMode string

const (
	NetworkModePublicInternet NetworkMode = "public_internet"
	NetworkModeDenyAll        NetworkMode = "deny_all"
)

// TimeoutAction represents the action taken when a workspace timeout expires.
type TimeoutAction string

const (
	TimeoutActionStop   TimeoutAction = "stop"
	TimeoutActionDelete TimeoutAction = "delete"
)

// WorkspaceTemplate identifies the template and revision used by a workspace.
type WorkspaceTemplate struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// WorkspaceResources describes the compute resources allocated to a workspace.
type WorkspaceResources struct {
	VCPUs     int `json:"vcpus"`
	MemoryMiB int `json:"memory_mib"`
	DiskMiB   int `json:"disk_mib"`
}

// WorkspaceNetwork describes a workspace's network policy and application state.
type WorkspaceNetwork struct {
	Mode      NetworkMode           `json:"mode"`
	Allow     []string              `json:"allow"`
	Deny      []string              `json:"deny"`
	Egresses  []NetworkEgressRule   `json:"egresses"`
	Resolvers []NetworkResolverRule `json:"resolvers"`
	Applied   *bool                 `json:"applied"` // nil when the workspace is not running
}

// NetworkEgressRule routes the listed IP addresses or CIDRs through an egress.
type NetworkEgressRule struct {
	EgressID     string   `json:"egress_id"`
	Destinations []string `json:"destinations"`
}

// NetworkResolverRule selects a DNS upstream and how to reach it.
type NetworkResolverRule struct {
	ResolverID string              `json:"resolver_id"`
	Path       NetworkResolverPath `json:"path"`
}

type NetworkResolverPath struct {
	Type     string `json:"type"` // direct or egress
	EgressID string `json:"egress_id,omitempty"`
}

// WorkspaceVolume describes a volume attached to a workspace.
type WorkspaceVolume struct {
	ID                  string `json:"id"`
	MountPath           string `json:"mount_path"`
	DeleteWithWorkspace bool   `json:"delete_with_workspace"`
}

// WorkspaceTimeout describes the active auto-timeout policy of a workspace.
type WorkspaceTimeout struct {
	ExpiresAt time.Time     `json:"expires_at"`
	Action    TimeoutAction `json:"action"`
}

// Workspace represents a Kibotos workspace resource.
type Workspace struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Zone      string             `json:"zone"`
	Template  WorkspaceTemplate  `json:"template"`
	Status    WorkspaceStatus    `json:"status"`
	Failure   *FailureInfo       `json:"failure"`
	Resources WorkspaceResources `json:"resources"`
	Network   WorkspaceNetwork   `json:"network"`
	Volume    *WorkspaceVolume   `json:"volume"`
	Volumes   []WorkspaceVolume  `json:"volumes"`
	Packs     []WorkspacePack    `json:"packs"`
	Timeout   *WorkspaceTimeout  `json:"timeout"`
	Labels    map[string]string  `json:"labels"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// WorkspacePack is a Pack and its resolved revision on a workspace.
type WorkspacePack struct {
	ID                   string   `json:"id"`
	Revision             string   `json:"revision"`
	ProvidedCapabilities []string `json:"provided_capabilities"`
}

// WorkspaceTemplateSpec specifies the template when creating a workspace.
type WorkspaceTemplateSpec struct {
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
}

// WorkspaceResourcesInput specifies optional compute overrides.
type WorkspaceResourcesInput struct {
	VCPUs     int `json:"vcpus,omitempty"`
	MemoryMiB int `json:"memory_mib,omitempty"`
	DiskMiB   int `json:"disk_mib,omitempty"`
}

type WorkspaceResourcesSpec = WorkspaceResourcesInput

// WorkspaceNetworkInput replaces the policy. DrainSeconds is only valid on SetNetwork.
type WorkspaceNetworkInput struct {
	Mode         NetworkMode           `json:"mode"`
	Allow        []string              `json:"allow,omitempty"`
	Deny         []string              `json:"deny,omitempty"`
	Egresses     []NetworkEgressRule   `json:"egresses,omitempty"`
	Resolvers    []NetworkResolverRule `json:"resolvers,omitempty"`
	DrainSeconds *int                  `json:"drain_seconds,omitempty"`
}

type WorkspaceNetworkSpec = WorkspaceNetworkInput

// WorkspaceVolumeInput specifies a volume binding.
type WorkspaceVolumeInput struct {
	ID                  string `json:"id"`
	MountPath           string `json:"mount_path,omitempty"`
	DeleteWithWorkspace bool   `json:"delete_with_workspace,omitempty"`
}

type WorkspaceVolumeSpec = WorkspaceVolumeInput

// TimeoutSpec configures a workspace timeout duration and action.
type TimeoutSpec struct {
	Seconds int           `json:"seconds"`
	Action  TimeoutAction `json:"action,omitempty"`
}

// CreateWorkspaceParams defines parameters for POST /v1/workspaces.
// Only Template.ID is required; all other fields are optional.
type CreateWorkspaceParams struct {
	Name      string                  `json:"name,omitempty"`
	Zone      string                  `json:"zone,omitempty"`
	Template  WorkspaceTemplateSpec   `json:"template"`
	Resources *WorkspaceResourcesSpec `json:"resources,omitempty"`
	Network   *WorkspaceNetworkSpec   `json:"network,omitempty"`
	Volume    *WorkspaceVolumeSpec    `json:"volume,omitempty"`
	Volumes   []WorkspaceVolumeInput  `json:"volumes,omitempty"`
	Packs     []string                `json:"packs,omitempty"`
	Timeout   *TimeoutSpec            `json:"timeout,omitempty"`
	Labels    map[string]string       `json:"labels,omitempty"`
}

type CreateWorkspaceRequest = CreateWorkspaceParams

// WorkspaceListParams defines optional filters and pagination for GET /v1/workspaces.
type WorkspaceListParams struct {
	Status    WorkspaceStatus
	Zone      string
	Labels    map[string]string
	Limit     int
	PageToken string
}

// UpdateWorkspaceParams defines parameters for PATCH /v1/workspaces/{id}.
// Use Some("new-name") to set Name, Null[string]() to clear Name, or leave Name unset to omit it.
// In Labels, non-nil values set a label and nil values delete the label key.
type UpdateWorkspaceParams struct {
	Name      Nullable[string]
	Labels    map[string]*string
	Resources *WorkspaceResourcesInput
}

type UpdateWorkspaceRequest = UpdateWorkspaceParams

// MarshalJSON encodes only the fields explicitly configured in UpdateWorkspaceParams.
func (p UpdateWorkspaceParams) MarshalJSON() ([]byte, error) {
	body := make(map[string]any, 3)
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
	if p.Resources != nil {
		body["resources"] = p.Resources
	}
	return json.Marshal(body)
}

// StartWorkspaceParams defines optional parameters for POST /v1/workspaces/{id}/start.
type StartWorkspaceParams struct {
	Timeout *TimeoutSpec `json:"timeout,omitempty"`
}

// StopWorkspaceParams defines optional parameters for POST /v1/workspaces/{id}/stop.
type StopWorkspaceParams struct {
	Force bool `json:"force,omitempty"`
}

// SetWorkspaceTimeoutParams defines parameters for POST /v1/workspaces/{id}/timeout.
type SetWorkspaceTimeoutParams struct {
	Seconds int           `json:"seconds"`
	Action  TimeoutAction `json:"action,omitempty"`
}

// SetWorkspaceNetworkParams defines parameters for PUT /v1/workspaces/{id}/network.
type SetWorkspaceNetworkParams = WorkspaceNetworkInput

// AttachVolumeParams defines parameters for PUT /v1/workspaces/{id}/volume.
type AttachVolumeParams struct {
	ID                  string `json:"id"`
	MountPath           string `json:"mount_path,omitempty"`
	DeleteWithWorkspace bool   `json:"delete_with_workspace,omitempty"`
}

// WaitWorkspaceParams defines parameters for GET /v1/workspaces/{id}/wait.
// If Timeout exceeds the server's 60-second chunk limit (or ctx has a longer deadline),
// Wait automatically loops across 60-second server wait chunks until a target status
// is reached, an unexpected_status error is returned, or the overall deadline expires.
type WaitWorkspaceParams struct {
	Statuses []WorkspaceStatus
	Timeout  time.Duration
}

// WorkspacesService handles communication with the workspaces endpoints.
type WorkspacesService struct {
	client *Client
}

// Create provisions a new workspace (POST /v1/workspaces).
func (s *WorkspacesService) Create(ctx context.Context, params CreateWorkspaceParams, opts ...RequestOption) (*Workspace, error) {
	if params.Template.ID == "" {
		return nil, errors.New("kibotos: template.id is required")
	}
	var ws Workspace
	if err := s.client.do(ctx, http.MethodPost, "/workspaces", nil, params, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// List returns a page of workspaces across zones (GET /v1/workspaces).
func (s *WorkspacesService) List(ctx context.Context, params *WorkspaceListParams, opts ...RequestOption) (*MultiZonePage[Workspace], error) {
	q := url.Values{}
	if params != nil {
		if params.Status != "" {
			q.Set("status", string(params.Status))
		}
		if params.Zone != "" {
			q.Set("zone", params.Zone)
		}
		addLabelParams(q, params.Labels)
		addPaginationParams(q, params.Limit, params.PageToken)
	}
	var page MultiZonePage[Workspace]
	if err := s.client.do(ctx, http.MethodGet, "/workspaces", q, nil, &page, opts...); err != nil {
		return nil, err
	}
	return &page, nil
}

// Iter returns an iterator over all matching workspaces across pages.
func (s *WorkspacesService) Iter(ctx context.Context, params *WorkspaceListParams, opts ...RequestOption) iter.Seq2[Workspace, error] {
	return func(yield func(Workspace, error) bool) {
		var p WorkspaceListParams
		if params != nil {
			p = *params
		}
		for {
			page, err := s.List(ctx, &p, opts...)
			if err != nil {
				yield(Workspace{}, err)
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

// Get retrieves a workspace by ID (GET /v1/workspaces/{id}).
func (s *WorkspacesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var ws Workspace
	if err := s.client.do(ctx, http.MethodGet, "/workspaces/"+url.PathEscape(id), nil, nil, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// Update modifies a workspace's name or labels (PATCH /v1/workspaces/{id}).
func (s *WorkspacesService) Update(ctx context.Context, id string, params UpdateWorkspaceParams, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var ws Workspace
	if err := s.client.do(ctx, http.MethodPatch, "/workspaces/"+url.PathEscape(id), nil, params, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// Delete initiates deletion of a workspace (DELETE /v1/workspaces/{id}).
func (s *WorkspacesService) Delete(ctx context.Context, id string, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var ws Workspace
	if err := s.client.do(ctx, http.MethodDelete, "/workspaces/"+url.PathEscape(id), nil, nil, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// Start starts a stopped workspace (POST /v1/workspaces/{id}/start).
func (s *WorkspacesService) Start(ctx context.Context, id string, params *StartWorkspaceParams, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var body any
	if params != nil {
		body = params
	}
	var ws Workspace
	path := "/workspaces/" + url.PathEscape(id) + "/start"
	if err := s.client.do(ctx, http.MethodPost, path, nil, body, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// Stop stops an available workspace (POST /v1/workspaces/{id}/stop).
func (s *WorkspacesService) Stop(ctx context.Context, id string, params *StopWorkspaceParams, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var body any
	if params != nil {
		body = params
	}
	var ws Workspace
	path := "/workspaces/" + url.PathEscape(id) + "/stop"
	if err := s.client.do(ctx, http.MethodPost, path, nil, body, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// SetTimeout sets or updates a workspace's auto-timeout (POST /v1/workspaces/{id}/timeout).
func (s *WorkspacesService) SetTimeout(ctx context.Context, id string, params SetWorkspaceTimeoutParams, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var ws Workspace
	path := "/workspaces/" + url.PathEscape(id) + "/timeout"
	if err := s.client.do(ctx, http.MethodPost, path, nil, params, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// SetNetwork updates a workspace's outbound network policy (PUT /v1/workspaces/{id}/network).
func (s *WorkspacesService) SetNetwork(ctx context.Context, id string, params WorkspaceNetworkInput, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var ws Workspace
	path := "/workspaces/" + url.PathEscape(id) + "/network"
	if err := s.client.do(ctx, http.MethodPut, path, nil, params, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// AttachVolume replaces all existing attachments with this single volume
// (PUT /v1/workspaces/{id}/volume). Use [WorkspacesService.SetVolumes] with the
// full list of bindings to keep multiple volumes attached.
func (s *WorkspacesService) AttachVolume(ctx context.Context, id string, params AttachVolumeParams, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	if params.ID == "" {
		return nil, errors.New("kibotos: volume id is required")
	}
	var ws Workspace
	path := "/workspaces/" + url.PathEscape(id) + "/volume"
	if err := s.client.do(ctx, http.MethodPut, path, nil, params, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

// DetachVolume detaches all volumes from a workspace (DELETE /v1/workspaces/{id}/volume).
// Use [WorkspacesService.SetVolumes] with the bindings to keep to detach a single
// volume from a workspace with multiple attachments.
func (s *WorkspacesService) DetachVolume(ctx context.Context, id string, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	var ws Workspace
	path := "/workspaces/" + url.PathEscape(id) + "/volume"
	if err := s.client.do(ctx, http.MethodDelete, path, nil, nil, &ws, opts...); err != nil {
		return nil, err
	}
	return &ws, nil
}

const (
	defaultWaitSeconds = 30
	maxWaitChunkSecs   = 60
)

// Wait blocks until the workspace reaches one of params.Statuses, fails with
// unexpected_status, or the timeout / context deadline expires.
func (s *WorkspacesService) Wait(ctx context.Context, id string, params WaitWorkspaceParams, opts ...RequestOption) (*Workspace, error) {
	if id == "" {
		return nil, errors.New("kibotos: workspace id is required")
	}
	if len(params.Statuses) == 0 {
		return nil, errors.New("kibotos: at least one target status is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	waitCtx, deadline, cancel := resolveWaitDeadline(ctx, params.Timeout)
	defer cancel()

	path := "/workspaces/" + url.PathEscape(id) + "/wait"
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

		var ws Workspace
		err := s.client.do(waitCtx, http.MethodGet, path, q, nil, &ws, reqOpts...)
		if err == nil {
			return &ws, nil
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

func resolveWaitDeadline(ctx context.Context, timeout time.Duration) (context.Context, time.Time, context.CancelFunc) {
	if timeout > 0 {
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		deadline, _ := waitCtx.Deadline()
		return waitCtx, deadline, cancel
	}
	if deadline, ok := ctx.Deadline(); ok {
		return ctx, deadline, func() {}
	}
	waitCtx, cancel := context.WithTimeout(ctx, defaultWaitSeconds*time.Second)
	deadline, _ := waitCtx.Deadline()
	return waitCtx, deadline, cancel
}

func computeWaitChunkSeconds(remaining time.Duration) int {
	secs := int(math.Ceil(remaining.Seconds()))
	if secs < 1 {
		return 1
	}
	if secs > maxWaitChunkSecs {
		return maxWaitChunkSecs
	}
	return secs
}

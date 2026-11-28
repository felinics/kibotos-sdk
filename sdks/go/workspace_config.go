package kibotos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

func workspaceEndpoint(id, suffix string) (string, error) {
	if id == "" {
		return "", errors.New("kibotos: workspace id is required")
	}
	return "/workspaces/" + url.PathEscape(id) + suffix, nil
}

// SetVolumes replaces all volume bindings on a stopped or failed workspace.
// Passing an empty or nil slice detaches all volumes.
func (s *WorkspacesService) SetVolumes(ctx context.Context, workspaceID string, volumes []WorkspaceVolumeInput, opts ...RequestOption) (*Workspace, error) {
	if volumes == nil {
		volumes = []WorkspaceVolumeInput{}
	}
	body := struct {
		Volumes []WorkspaceVolumeInput `json:"volumes"`
	}{volumes}
	return s.replaceWorkspaceConfig(ctx, workspaceID, "/volumes", body, opts)
}

// SetPacks replaces the ordered list of Packs. The list takes effect on the next start.
// Passing an empty or nil slice clears the list.
func (s *WorkspacesService) SetPacks(ctx context.Context, workspaceID string, packs []string, opts ...RequestOption) (*Workspace, error) {
	if packs == nil {
		packs = []string{}
	}
	body := struct {
		Packs []string `json:"packs"`
	}{packs}
	return s.replaceWorkspaceConfig(ctx, workspaceID, "/packs", body, opts)
}

func (s *WorkspacesService) replaceWorkspaceConfig(ctx context.Context, id, suffix string, body any, opts []RequestOption) (*Workspace, error) {
	path, err := workspaceEndpoint(id, suffix)
	if err != nil {
		return nil, err
	}
	return requestJSON[Workspace](ctx, s.client, http.MethodPut, path, nil, body, opts)
}

// WorkspaceMetricsOptions specifies an optional metrics interval.
type WorkspaceMetricsOptions struct {
	Start time.Time
	End   time.Time
}

type WorkspaceGuestMetricsPoint struct {
	Time             time.Time `json:"time"`
	CPUCount         int       `json:"cpu_count"`
	CPUUsedPercent   float64   `json:"cpu_used_percent"`
	DiskTotalBytes   int64     `json:"disk_total_bytes"`
	DiskUsedBytes    int64     `json:"disk_used_bytes"`
	MemoryCacheBytes int64     `json:"memory_cache_bytes"`
	MemoryTotalBytes int64     `json:"memory_total_bytes"`
	MemoryUsedBytes  int64     `json:"memory_used_bytes"`
}

// WorkspaceHostMetricsPoint uses nil for measurements that were unavailable.
type WorkspaceHostMetricsPoint struct {
	Time                            time.Time `json:"time"`
	CPUCores                        *float64  `json:"cpu_cores,omitempty"`
	CPUPressureSomeRatio            *float64  `json:"cpu_pressure_some_ratio,omitempty"`
	IOPressureFullRatio             *float64  `json:"io_pressure_full_ratio,omitempty"`
	IOPressureSomeRatio             *float64  `json:"io_pressure_some_ratio,omitempty"`
	MemoryBytes                     *int64    `json:"memory_bytes,omitempty"`
	MemoryPressureFullRatio         *float64  `json:"memory_pressure_full_ratio,omitempty"`
	MemoryPressureSomeRatio         *float64  `json:"memory_pressure_some_ratio,omitempty"`
	NetworkReceiveBytesPerSecond    *float64  `json:"network_receive_bytes_per_second,omitempty"`
	NetworkReceiveDroppedPerSecond  *float64  `json:"network_receive_dropped_per_second,omitempty"`
	NetworkReceivePacketsPerSecond  *float64  `json:"network_receive_packets_per_second,omitempty"`
	NetworkTransmitBytesPerSecond   *float64  `json:"network_transmit_bytes_per_second,omitempty"`
	NetworkTransmitDroppedPerSecond *float64  `json:"network_transmit_dropped_per_second,omitempty"`
	NetworkTransmitPacketsPerSecond *float64  `json:"network_transmit_packets_per_second,omitempty"`
}

type WorkspaceMetrics struct {
	EarliestTime *time.Time                   `json:"earliest_time"`
	Guest        []WorkspaceGuestMetricsPoint `json:"guest"`
	Host         []WorkspaceHostMetricsPoint  `json:"host"`
	StepSeconds  int                          `json:"step_seconds"`
}

// GetMetrics reads host and guest time series, including retained metrics for deleted workspaces.
func (s *WorkspacesService) GetMetrics(ctx context.Context, workspaceID string, options *WorkspaceMetricsOptions, opts ...RequestOption) (*WorkspaceMetrics, error) {
	path, err := workspaceEndpoint(workspaceID, "/metrics")
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if options != nil {
		if !options.Start.IsZero() {
			query.Set("start", options.Start.Format(time.RFC3339Nano))
		}
		if !options.End.IsZero() {
			query.Set("end", options.End.Format(time.RFC3339Nano))
		}
	}
	return requestJSON[WorkspaceMetrics](ctx, s.client, http.MethodGet, path, query, nil, opts)
}

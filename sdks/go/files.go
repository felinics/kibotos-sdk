package kibotos

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type FileEntry struct {
	Path       string     `json:"path"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Mode       string     `json:"mode"`
	SizeBytes  int64      `json:"size_bytes"`
	ModifiedAt *time.Time `json:"modified_at"`
}

type FileList struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

type FileWriteResult struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type ListFilesOptions struct {
	User  string
	Depth int
}

type FileOptions struct {
	User string
}

type ReadFileOptions struct {
	User        string
	Range       string
	IfNoneMatch string
}

// FileContent streams the server body and exposes HTTP range and cache headers.
// Close it when done. StatusCode can be 200, 206, or 304 (empty body).
type FileContent struct {
	io.ReadCloser
	StatusCode    int
	ContentLength int64
	Header        http.Header
}

type WriteFileOptions struct {
	Mode       string
	CreateOnly bool
	User       string
}

type MakeDirectoryRequest struct {
	Path string `json:"path"`
	Mode string `json:"mode,omitempty"`
	User string `json:"user,omitempty"`
}

type MoveFileRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	User        string `json:"user,omitempty"`
}

func fileEndpoint(id, suffix, path, user string) (string, url.Values, error) {
	if path == "" {
		return "", nil, errors.New("kibotos: file path is required")
	}
	endpoint, err := workspaceEndpoint(id, suffix)
	query := url.Values{"path": {path}}
	if user != "" {
		query.Set("user", user)
	}
	return endpoint, query, err
}

// ListFiles lists a directory. A relative path resolves against the selected user's home.
func (s *WorkspacesService) ListFiles(ctx context.Context, workspaceID, path string, options ListFilesOptions, opts ...RequestOption) (*FileList, error) {
	endpoint, query, err := fileEndpoint(workspaceID, "/files", path, options.User)
	if err != nil {
		return nil, err
	}
	if options.Depth != 0 {
		query.Set("depth", strconv.Itoa(options.Depth))
	}
	return requestJSON[FileList](ctx, s.client, http.MethodGet, endpoint, query, nil, opts)
}

func (s *WorkspacesService) StatFile(ctx context.Context, workspaceID, path string, options FileOptions, opts ...RequestOption) (*FileEntry, error) {
	endpoint, query, err := fileEndpoint(workspaceID, "/files/stat", path, options.User)
	if err != nil {
		return nil, err
	}
	return requestJSON[FileEntry](ctx, s.client, http.MethodGet, endpoint, query, nil, opts)
}

// ReadFile opens a download. Cancel ctx to interrupt the transfer.
func (s *WorkspacesService) ReadFile(ctx context.Context, workspaceID, path string, options ReadFileOptions, opts ...RequestOption) (*FileContent, error) {
	endpoint, query, err := fileEndpoint(workspaceID, "/files/content", path, options.User)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	if options.Range != "" {
		headers.Set("Range", options.Range)
	}
	if options.IfNoneMatch != "" {
		headers.Set("If-None-Match", options.IfNoneMatch)
	}
	resp, err := s.client.openDataRequest(ctx, http.MethodGet, endpoint, query, nil, "", "application/octet-stream", headers, opts)
	if err != nil {
		return nil, err
	}
	return &FileContent{resp.Body, resp.StatusCode, resp.ContentLength, resp.Header}, nil
}

// WriteFile streams input once. Cancel ctx to interrupt the transfer.
func (s *WorkspacesService) WriteFile(ctx context.Context, workspaceID, path string, input io.Reader, options WriteFileOptions, opts ...RequestOption) (*FileWriteResult, error) {
	if input == nil {
		return nil, errors.New("kibotos: file input is required")
	}
	endpoint, query, err := fileEndpoint(workspaceID, "/files/content", path, options.User)
	if err != nil {
		return nil, err
	}
	if options.Mode != "" {
		query.Set("mode", options.Mode)
	}
	if options.CreateOnly {
		query.Set("create_only", "true")
	}
	resp, err := s.client.openDataRequest(ctx, http.MethodPut, endpoint, query, input, "application/octet-stream", "application/json", nil, opts)
	if err != nil {
		return nil, err
	}
	return decodeDataResponse[FileWriteResult](resp)
}

func (s *WorkspacesService) MakeDir(ctx context.Context, workspaceID string, body MakeDirectoryRequest, opts ...RequestOption) (*FileEntry, error) {
	if body.Path == "" {
		return nil, errors.New("kibotos: file path is required")
	}
	return s.fileMutation(ctx, workspaceID, "/files/mkdir", body, opts)
}

func (s *WorkspacesService) MoveFile(ctx context.Context, workspaceID string, body MoveFileRequest, opts ...RequestOption) (*FileEntry, error) {
	if body.Source == "" || body.Destination == "" {
		return nil, errors.New("kibotos: source and destination are required")
	}
	return s.fileMutation(ctx, workspaceID, "/files/move", body, opts)
}

func (s *WorkspacesService) fileMutation(ctx context.Context, id, suffix string, body any, opts []RequestOption) (*FileEntry, error) {
	path, payload, err := workspaceJSONBody(id, suffix, body)
	if err != nil {
		return nil, err
	}
	if s.client.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.client.timeout)
		defer cancel()
	}
	resp, err := s.client.openDataRequest(ctx, http.MethodPost, path, nil, payload, "application/json", "application/json", nil, opts)
	if err != nil {
		return nil, err
	}
	return decodeDataResponse[FileEntry](resp)
}

func (s *WorkspacesService) DeleteFile(ctx context.Context, workspaceID, path string, options FileOptions, opts ...RequestOption) error {
	endpoint, query, err := fileEndpoint(workspaceID, "/files", path, options.User)
	if err != nil {
		return err
	}
	if s.client.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.client.timeout)
		defer cancel()
	}
	resp, err := s.client.openDataRequest(ctx, http.MethodDelete, endpoint, query, nil, "", "application/json", nil, opts)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

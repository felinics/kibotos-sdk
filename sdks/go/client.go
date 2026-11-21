package kibotos

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the default production Kibotos v1 API base URL.
	DefaultBaseURL = "https://api.kibo.to/v1"
	// DefaultTimeout is the default per-request timeout.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxRetries is the default maximum number of retry attempts.
	DefaultMaxRetries = 3
	// Version is the Go SDK version.
	Version = "0.1.0"

	defaultMinBackoff = 500 * time.Millisecond
	defaultMaxBackoff = 8 * time.Second
)

// Client is the Kibotos v1 API client.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	maxRetries int
	userAgent  string
	minBackoff time.Duration
	maxBackoff time.Duration

	Zones      *ZonesService
	Templates  *TemplatesService
	Workspaces *WorkspacesService
	Volumes    *VolumesService
	Exposures  *ExposuresService
}

// ClientOption configures a Client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	baseURL       string
	httpClient    *http.Client
	timeout       time.Duration
	timeoutSet    bool
	maxRetries    int
	maxRetriesSet bool
	userAgent     string
	minBackoff    time.Duration
	maxBackoff    time.Duration
}

// WithBaseURL overrides the API base URL. Both "https://api.kibo.to" and
// "https://api.kibo.to/v1" are normalized cleanly to end with "/v1".
func WithBaseURL(baseURL string) ClientOption {
	return func(c *clientConfig) {
		c.baseURL = baseURL
	}
}

// WithHTTPClient sets a custom *http.Client for API requests.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *clientConfig) {
		c.httpClient = hc
	}
}

// WithMaxRetries sets the maximum number of retry attempts after the initial request.
// Negative values are clamped to 0.
func WithMaxRetries(n int) ClientOption {
	return func(c *clientConfig) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
		c.maxRetriesSet = true
	}
}

// WithTimeout sets the default per-request timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.timeout = d
		c.timeoutSet = true
	}
}

// WithUserAgent sets a custom User-Agent header value.
func WithUserAgent(ua string) ClientOption {
	return func(c *clientConfig) {
		c.userAgent = ua
	}
}

// WithRetryBackoff sets the minimum and maximum exponential backoff delays for retries.
func WithRetryBackoff(minDelay, maxDelay time.Duration) ClientOption {
	return func(c *clientConfig) {
		if minDelay > 0 {
			c.minBackoff = minDelay
		}
		if maxDelay > 0 {
			c.maxBackoff = maxDelay
		}
	}
}

// NewClient creates a new Kibotos v1 API client with the given API key and options.
func NewClient(apiKey string, opts ...ClientOption) (*Client, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("kibotos: API key is required")
	}

	cfg := clientConfig{
		baseURL:    DefaultBaseURL,
		timeout:    DefaultTimeout,
		maxRetries: DefaultMaxRetries,
		userAgent:  "kibotos-go/" + Version,
		minBackoff: defaultMinBackoff,
		maxBackoff: defaultMaxBackoff,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	normalizedURL, err := NormalizeBaseURL(cfg.baseURL)
	if err != nil {
		return nil, err
	}

	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{}
	}

	if cfg.maxBackoff < cfg.minBackoff {
		cfg.maxBackoff = cfg.minBackoff
	}

	c := &Client{
		apiKey:     apiKey,
		baseURL:    normalizedURL,
		httpClient: hc,
		timeout:    cfg.timeout,
		maxRetries: cfg.maxRetries,
		userAgent:  cfg.userAgent,
		minBackoff: cfg.minBackoff,
		maxBackoff: cfg.maxBackoff,
	}
	c.Zones = &ZonesService{client: c}
	c.Templates = &TemplatesService{client: c}
	c.Workspaces = &WorkspacesService{client: c}
	c.Volumes = &VolumesService{client: c}
	c.Exposures = &ExposuresService{client: c}
	return c, nil
}

// NewClientFromEnv creates a Client from KIBOTOS_API_KEY and optional KIBOTOS_API_URL.
// Explicit opts take precedence over environment variables.
func NewClientFromEnv(opts ...ClientOption) (*Client, error) {
	apiKey := strings.TrimSpace(os.Getenv("KIBOTOS_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("kibotos: KIBOTOS_API_KEY environment variable is not set")
	}

	var allOpts []ClientOption
	if envURL := strings.TrimSpace(os.Getenv("KIBOTOS_API_URL")); envURL != "" {
		allOpts = append(allOpts, WithBaseURL(envURL))
	}
	allOpts = append(allOpts, opts...)
	return NewClient(apiKey, allOpts...)
}

// BaseURL returns the normalized base URL (ending in "/v1") used by the client.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// NormalizeBaseURL normalizes an API base URL so it has no trailing slash and ends with "/v1".
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("kibotos: invalid base URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("kibotos: base URL %q must use http or https scheme", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("kibotos: base URL %q must include a host", raw)
	}

	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/v1") {
		u.Path += "/v1"
	}
	return u.String(), nil
}

// RequestOption customizes a single API request.
type RequestOption func(*requestOptions)

type requestOptions struct {
	idempotencyKey   string
	headers          http.Header
	skipTimeoutRetry bool
	perAttemptLimit  time.Duration
}

// WithIdempotencyKey sets the Idempotency-Key header on a request.
func WithIdempotencyKey(key string) RequestOption {
	return func(o *requestOptions) {
		o.idempotencyKey = key
	}
}

// WithHeader adds a custom HTTP header to a single request.
func WithHeader(key, value string) RequestOption {
	return func(o *requestOptions) {
		if o.headers == nil {
			o.headers = make(http.Header)
		}
		o.headers.Set(key, value)
	}
}

func withWaitRequestConfig(perAttempt time.Duration) RequestOption {
	return func(o *requestOptions) {
		o.skipTimeoutRetry = true
		o.perAttemptLimit = perAttempt
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any, opts ...RequestOption) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var reqOpts requestOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&reqOpts)
		}
	}

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kibotos: marshal request body: %w", err)
		}
	}

	// Auto-generate a UUID v4 Idempotency-Key on POST mutations when omitted,
	// and reuse the exact same key across retry attempts.
	idempotencyKey := reqOpts.idempotencyKey
	if idempotencyKey == "" && reqOpts.headers != nil {
		idempotencyKey = reqOpts.headers.Get("Idempotency-Key")
	}
	if idempotencyKey == "" && method == http.MethodPost {
		var err error
		idempotencyKey, err = newUUIDv4()
		if err != nil {
			return fmt.Errorf("kibotos: generate idempotency key: %w", err)
		}
	}

	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	perAttemptTimeout := c.timeout
	if reqOpts.perAttemptLimit > 0 {
		perAttemptTimeout = reqOpts.perAttemptLimit
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		attemptCtx := ctx
		var cancel context.CancelFunc
		if perAttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, perAttemptTimeout)
		}

		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(attemptCtx, method, fullURL, bodyReader)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			return fmt.Errorf("kibotos: build request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if c.userAgent != "" {
			req.Header.Set("User-Agent", c.userAgent)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		for k, vals := range reqOpts.headers {
			for _, v := range vals {
				req.Header.Set(k, v)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			lastErr = err
			if attempt < c.maxRetries && ctx.Err() == nil && isRetryableNetworkErr(err) {
				if sleepErr := sleepWithContext(ctx, c.backoffDelay(attempt)); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return err
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		if readErr != nil {
			lastErr = readErr
			if attempt < c.maxRetries && ctx.Err() == nil && isRetryableNetworkErr(readErr) {
				if sleepErr := sleepWithContext(ctx, c.backoffDelay(attempt)); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return readErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, out); err != nil {
					return fmt.Errorf("kibotos: decode response: %w", err)
				}
			}
			return nil
		}

		apiErr := parseAPIError(resp.StatusCode, resp.Header, respBody)
		lastErr = apiErr

		if reqOpts.skipTimeoutRetry && IsWaitTimeout(apiErr) {
			return apiErr
		}

		shouldRetry := apiErr.Retryable || isRetryableStatus(resp.StatusCode) ||
			(!reqOpts.skipTimeoutRetry && resp.StatusCode == http.StatusRequestTimeout)

		if attempt < c.maxRetries && shouldRetry {
			delay := c.backoffDelay(attempt)
			if resp.StatusCode == http.StatusTooManyRequests {
				if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
					delay = ra
				}
			}
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return sleepErr
			}
			continue
		}

		return apiErr
	}

	return lastErr
}

func (c *Client) backoffDelay(attempt int) time.Duration {
	exp := float64(c.minBackoff) * math.Pow(2, float64(attempt))
	if exp > float64(c.maxBackoff) {
		exp = float64(c.maxBackoff)
	}
	// Apply jitter in [0.5*exp, 1.0*exp].
	jittered := exp * (0.5 + 0.5*mathrand.Float64())
	return time.Duration(jittered)
}

func parseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(header, 64); err == nil {
		if secs <= 0 {
			return 0, true
		}
		return time.Duration(secs * float64(time.Second)), true
	}
	if t, err := http.ParseTime(header); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

func isRetryableNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

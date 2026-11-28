package kibotos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type EgressProtocol string

const EgressProtocolSocks5 EgressProtocol = "socks5"

type EgressTLS struct {
	ServerName *string `json:"server_name,omitempty"`
	CACert     *string `json:"ca_cert,omitempty"`
}

type EgressTLSInput = EgressTLS

type EgressSocks5 struct {
	Address  string     `json:"address"`
	Username *string    `json:"username"`
	TLS      *EgressTLS `json:"tls"`
}

type EgressSocks5Input struct {
	Address  string          `json:"address"`
	Username *string         `json:"username,omitempty"`
	Password *string         `json:"password,omitempty"` // write-only
	TLS      *EgressTLSInput `json:"tls,omitempty"`
}

type Egress struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Zone       string            `json:"zone"`
	Protocol   EgressProtocol    `json:"protocol"`
	Socks5     EgressSocks5      `json:"socks5"`
	UDPEnabled bool              `json:"udp_enabled"`
	Revision   int64             `json:"revision"`
	Labels     map[string]string `json:"labels"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type CreateEgressRequest struct {
	Name       string            `json:"name,omitempty"`
	Zone       string            `json:"zone,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Protocol   EgressProtocol    `json:"protocol"`
	Socks5     EgressSocks5Input `json:"socks5"`
	UDPEnabled bool              `json:"udp_enabled"`
}

type ReplaceEgressRequest struct {
	Protocol     EgressProtocol    `json:"protocol"`
	Socks5       EgressSocks5Input `json:"socks5"`
	UDPEnabled   bool              `json:"udp_enabled"`
	DrainSeconds *int              `json:"drain_seconds,omitempty"`
}

type ResolverProtocol string

const (
	ResolverProtocolUDP   ResolverProtocol = "udp"
	ResolverProtocolTCP   ResolverProtocol = "tcp"
	ResolverProtocolTLS   ResolverProtocol = "tls"
	ResolverProtocolHTTPS ResolverProtocol = "https"
)

type ResolverAddress struct {
	Address string `json:"address"`
}

type ResolverHTTPS struct {
	URL string `json:"url"`
}

type Resolver struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Zone      string            `json:"zone"`
	Revision  int64             `json:"revision"`
	Labels    map[string]string `json:"labels"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	ReplaceResolverRequest
}

type CreateResolverRequest struct {
	Name     string            `json:"name,omitempty"`
	Zone     string            `json:"zone,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Protocol ResolverProtocol  `json:"protocol"`
	UDP      *ResolverAddress  `json:"udp,omitempty"`
	TCP      *ResolverAddress  `json:"tcp,omitempty"`
	TLS      *ResolverAddress  `json:"tls,omitempty"`
	HTTPS    *ResolverHTTPS    `json:"https,omitempty"`
}

// ReplaceResolverRequest selects exactly one upstream matching Protocol.
type ReplaceResolverRequest struct {
	Protocol ResolverProtocol `json:"protocol"`
	UDP      *ResolverAddress `json:"udp,omitempty"`
	TCP      *ResolverAddress `json:"tcp,omitempty"`
	TLS      *ResolverAddress `json:"tls,omitempty"`
	HTTPS    *ResolverHTTPS   `json:"https,omitempty"`
}

// NetworkResourceUpdate changes the name and merge-patches labels. Use Null to
// clear the name and nil label values to remove keys.
type NetworkResourceUpdate struct {
	Name   Nullable[string]
	Labels map[string]*string
}

func (p NetworkResourceUpdate) MarshalJSON() ([]byte, error) {
	body := make(map[string]any, 2)
	if p.Name.Set {
		body["name"] = p.Name
	}
	if p.Labels != nil {
		body["labels"] = p.Labels
	}
	return json.Marshal(body)
}

type UpdateEgressRequest = NetworkResourceUpdate
type UpdateResolverRequest = NetworkResourceUpdate

type NetworkResourceListParams struct {
	Zone      string
	Labels    map[string]string
	Limit     int
	PageToken string
}

type EgressListParams = NetworkResourceListParams
type ResolverListParams = NetworkResourceListParams

func (p *NetworkResourceListParams) query() url.Values {
	query := url.Values{}
	if p != nil {
		addPaginationParams(query, p.Limit, p.PageToken)
		addLabelParams(query, p.Labels)
		if p.Zone != "" {
			query.Set("zone", p.Zone)
		}
	}
	return query
}

// EgressesService manages SOCKS5 egresses.
type EgressesService struct{ client *Client }

func (s *EgressesService) Create(ctx context.Context, body CreateEgressRequest, opts ...RequestOption) (*Egress, error) {
	return requestJSON[Egress](ctx, s.client, http.MethodPost, "/egresses", nil, body, opts)
}

func (s *EgressesService) List(ctx context.Context, params *EgressListParams, opts ...RequestOption) (*MultiZonePage[Egress], error) {
	return requestJSON[MultiZonePage[Egress]](ctx, s.client, http.MethodGet, "/egresses", params.query(), nil, opts)
}

func (s *EgressesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Egress, error) {
	return networkResourceRequest[Egress](ctx, s.client, http.MethodGet, "egress", "/egresses", id, nil, opts)
}

func (s *EgressesService) Replace(ctx context.Context, id string, body ReplaceEgressRequest, opts ...RequestOption) (*Egress, error) {
	return networkResourceRequest[Egress](ctx, s.client, http.MethodPut, "egress", "/egresses", id, body, opts)
}

func (s *EgressesService) Update(ctx context.Context, id string, body UpdateEgressRequest, opts ...RequestOption) (*Egress, error) {
	return networkResourceRequest[Egress](ctx, s.client, http.MethodPatch, "egress", "/egresses", id, body, opts)
}

func (s *EgressesService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	_, err := networkResourceRequest[Egress](ctx, s.client, http.MethodDelete, "egress", "/egresses", id, nil, opts)
	return err
}

// ResolversService manages custom DNS upstreams.
type ResolversService struct{ client *Client }

func (s *ResolversService) Create(ctx context.Context, body CreateResolverRequest, opts ...RequestOption) (*Resolver, error) {
	return requestJSON[Resolver](ctx, s.client, http.MethodPost, "/resolvers", nil, body, opts)
}

func (s *ResolversService) List(ctx context.Context, params *ResolverListParams, opts ...RequestOption) (*MultiZonePage[Resolver], error) {
	return requestJSON[MultiZonePage[Resolver]](ctx, s.client, http.MethodGet, "/resolvers", params.query(), nil, opts)
}

func (s *ResolversService) Get(ctx context.Context, id string, opts ...RequestOption) (*Resolver, error) {
	return networkResourceRequest[Resolver](ctx, s.client, http.MethodGet, "resolver", "/resolvers", id, nil, opts)
}

func (s *ResolversService) Replace(ctx context.Context, id string, body ReplaceResolverRequest, opts ...RequestOption) (*Resolver, error) {
	return networkResourceRequest[Resolver](ctx, s.client, http.MethodPut, "resolver", "/resolvers", id, body, opts)
}

func (s *ResolversService) Update(ctx context.Context, id string, body UpdateResolverRequest, opts ...RequestOption) (*Resolver, error) {
	return networkResourceRequest[Resolver](ctx, s.client, http.MethodPatch, "resolver", "/resolvers", id, body, opts)
}

func (s *ResolversService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	_, err := networkResourceRequest[Resolver](ctx, s.client, http.MethodDelete, "resolver", "/resolvers", id, nil, opts)
	return err
}

func networkResourceRequest[T any](ctx context.Context, c *Client, method, resource, collection, id string, body any, opts []RequestOption) (*T, error) {
	if id == "" {
		return nil, fmt.Errorf("kibotos: %s id is required", resource)
	}
	if method == http.MethodPut {
		opts = append([]RequestOption{func(o *requestOptions) { o.generateKey = true }}, opts...)
	}
	return requestJSON[T](ctx, c, method, collection+"/"+url.PathEscape(id), nil, body, opts)
}

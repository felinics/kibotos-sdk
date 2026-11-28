package kibotos_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientForHandler(t *testing.T, handler http.HandlerFunc, opts ...kibotos.ClientOption) *kibotos.Client {
	t.Helper()
	hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil {
			defer req.Body.Close()
		}
		w := httptest.NewRecorder()
		handler(w, req)
		return w.Result(), nil
	})}
	client, err := kibotos.NewClient("kb_wave3", append([]kibotos.ClientOption{kibotos.WithHTTPClient(hc)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func checkJSONBody(t *testing.T, r *http.Request, want string) {
	t.Helper()
	var got, expected any
	if r.Body == nil {
		t.Fatal("missing request body")
	}
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("body = %#v, want %#v", got, expected)
	}
}

type controlCase struct {
	name, method, path, query, body, result string
	call                                    func(context.Context, *kibotos.Client) (any, error)
}

func runControlCases(t *testing.T, cases []controlCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.EscapedPath() != tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("request = %s %s?%s, want %s %s?%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery, tc.method, tc.path, tc.query)
				}
				if r.Header.Get("Authorization") != "Bearer kb_wave3" {
					t.Errorf("authorization = %q", r.Header.Get("Authorization"))
				}
				if (r.Method == "POST" || (r.Method == "PUT" && !strings.Contains(tc.path, "/workspaces/"))) && !strings.Contains(tc.path, "/files/") &&
					!uuidV4Re.MatchString(r.Header.Get("Idempotency-Key")) {
					t.Errorf("expected generated idempotency key, got %q", r.Header.Get("Idempotency-Key"))
				}
				if tc.body != "" {
					checkJSONBody(t, r, tc.body)
				}
				if tc.result == "" {
					w.WriteHeader(http.StatusNoContent)
				} else {
					_, _ = io.WriteString(w, tc.result)
				}
			})
			result, err := tc.call(context.Background(), c)
			if err != nil || calls != 1 {
				t.Fatalf("result = %+v, err = %v, calls = %d", result, err, calls)
			}
			if tc.result == "" && result != nil {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestWave3WorkspaceConfiguration(t *testing.T) {
	const root = "/v1/workspaces/wks_1"
	from := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	runControlCases(t, []controlCase{
		{"create", "POST", "/v1/workspaces", "", `{"template":{"id":"base"},"packs":["desktop"],"volumes":[{"id":"vol_1","mount_path":"/data"}]}`,
			`{"volume":{"id":"vol_1"},"volumes":[{"id":"vol_1"}],"packs":[{"id":"desktop","revision":"v1","provided_capabilities":["desktop.v1"]}]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				ws, err := c.Workspaces.Create(ctx, kibotos.CreateWorkspaceRequest{
					Template: kibotos.WorkspaceTemplateSpec{ID: "base"}, Packs: []string{"desktop"},
					Volumes: []kibotos.WorkspaceVolumeInput{{ID: "vol_1", MountPath: "/data"}},
				})
				if err == nil && (ws.Volume == nil || len(ws.Volumes) != 1 || len(ws.Packs) != 1 ||
					ws.Volume.ID != ws.Volumes[0].ID || ws.Packs[0].ProvidedCapabilities[0] != "desktop.v1") {
					t.Errorf("unexpected workspace: %+v", ws)
				}
				return ws, err
			}},
		{"update", "PATCH", root, "", `{"name":null,"labels":{"drop":null},"resources":{"memory_mib":2048,"disk_mib":4096}}`, `{"id":"wks_1"}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.Update(ctx, "wks_1", kibotos.UpdateWorkspaceRequest{
					Name: kibotos.Null[string](), Labels: map[string]*string{"drop": nil},
					Resources: &kibotos.WorkspaceResourcesInput{MemoryMiB: 2048, DiskMiB: 4096},
				})
			}},
		{"network", "PUT", root + "/network", "",
			`{"mode":"public_internet","allow":["10.0.0.1"],"deny":["192.0.2.0/24"],"egresses":[{"egress_id":"egr_1","destinations":["10.0.0.0/8"]}],"resolvers":[{"resolver_id":"rsv_1","path":{"type":"egress","egress_id":"egr_1"}}],"drain_seconds":0}`,
			`{"network":{"applied":null,"allow":["10.0.0.1"],"deny":["192.0.2.0/24"],"egresses":[{"egress_id":"egr_1"}],"resolvers":[{"resolver_id":"rsv_1","path":{"type":"egress","egress_id":"egr_1"}}]}}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				ws, err := c.Workspaces.SetNetwork(ctx, "wks_1", kibotos.WorkspaceNetworkInput{
					Mode: kibotos.NetworkModePublicInternet, Allow: []string{"10.0.0.1"}, Deny: []string{"192.0.2.0/24"},
					Egresses:     []kibotos.NetworkEgressRule{{EgressID: "egr_1", Destinations: []string{"10.0.0.0/8"}}},
					Resolvers:    []kibotos.NetworkResolverRule{{ResolverID: "rsv_1", Path: kibotos.NetworkResolverPath{Type: "egress", EgressID: "egr_1"}}},
					DrainSeconds: kibotos.Ptr(0),
				})
				if err == nil && (ws.Network.Applied != nil || len(ws.Network.Allow) != 1 || len(ws.Network.Deny) != 1 ||
					len(ws.Network.Egresses) != 1 || len(ws.Network.Resolvers) != 1) {
					t.Errorf("unexpected decoded network: %+v", ws.Network)
				}
				return ws, err
			}},
		{"volumes", "PUT", root + "/volumes", "", `{"volumes":[{"id":"vol_1","mount_path":"/data","delete_with_workspace":true}]}`, `{"id":"wks_1"}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.SetVolumes(ctx, "wks_1", []kibotos.WorkspaceVolumeInput{{ID: "vol_1", MountPath: "/data", DeleteWithWorkspace: true}})
			}},
		{"clear volumes", "PUT", root + "/volumes", "", `{"volumes":[]}`, `{"id":"wks_1"}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.SetVolumes(ctx, "wks_1", nil)
			}},
		{"packs", "PUT", root + "/packs", "", `{"packs":["a","b"]}`, `{"id":"wks_1"}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.SetPacks(ctx, "wks_1", []string{"a", "b"})
			}},
		{"clear packs", "PUT", root + "/packs", "", `{"packs":[]}`, `{"id":"wks_1"}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.SetPacks(ctx, "wks_1", nil)
			}},
		{"metrics", "GET", root + "/metrics",
			url.Values{"start": {from.Format(time.RFC3339)}, "end": {to.Format(time.RFC3339)}}.Encode(), "",
			`{"step_seconds":30,"earliest_time":"2026-11-16T10:00:00Z","host":[{"time":"2026-11-16T10:00:00Z","cpu_cores":0,"network_receive_bytes_per_second":12}],"guest":[{"time":"2026-11-16T10:00:00Z","memory_total_bytes":4294967296,"cpu_used_percent":5.25}]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				m, err := c.Workspaces.GetMetrics(ctx, "wks_1", &kibotos.WorkspaceMetricsOptions{Start: from, End: to})
				if err == nil && (m.EarliestTime == nil || !m.EarliestTime.Equal(from) || m.StepSeconds != 30 || len(m.Host) != 1 ||
					m.Host[0].CPUCores == nil || *m.Host[0].CPUCores != 0 || m.Host[0].MemoryBytes != nil ||
					len(m.Guest) != 1 || m.Guest[0].MemoryTotalBytes != 4294967296 || m.Guest[0].CPUUsedPercent != 5.25) {
					t.Errorf("unexpected metrics: %+v", m)
				}
				return m, err
			}},
	})
}

func TestWave3CatalogAndNetworkResources(t *testing.T) {
	const egress = `{"id":"egr_1","revision":2,"socks5":{"address":"host:1080","password":"server-must-not-expose"}}`
	const resolver = `{"id":"rsv_1","revision":3,"protocol":"https","https":{"url":"https://dns.example/dns-query"}}`
	filters := &kibotos.NetworkResourceListParams{Zone: "global-1", Labels: map[string]string{"env": "prod"}, Limit: 5, PageToken: "next"}
	runControlCases(t, []controlCase{
		{"list packs", "GET", "/v1/packs", "limit=5&page_token=next&zone=global-1", "",
			`{"data":[{"id":"desk","default_revision":"r1"}],"next_page_token":"next-2","unavailable_zones":["global-2"]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				p, err := c.Packs.List(ctx, &kibotos.PackListParams{Zone: "global-1", Limit: 5, PageToken: "next"})
				if err == nil && (p.Token() != "next-2" || len(p.Data) != 1 || p.Data[0].DefaultRevision != "r1" || len(p.UnavailableZones) != 1) {
					t.Errorf("unexpected Pack page: %+v", p)
				}
				return p, err
			}},
		{"get pack", "GET", "/v1/packs/desk%2Ftool", "", "", `{"id":"desk/tool"}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) { return c.Packs.Get(ctx, "desk/tool") }},
		{"get pack revision", "GET", "/v1/packs/desk/revisions/r%2F1", "", "", `{"id":"r/1","pack_id":"desk","zones":["global-1"]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Packs.GetRevision(ctx, "desk", "r/1")
			}},
		{"create egress", "POST", "/v1/egresses", "",
			`{"name":"egress","zone":"global-1","socks5":{"address":"host:1080","username":"agent","password":"input-only","tls":{"server_name":"host"}},"udp_enabled":true,"protocol":"socks5","labels":{"env":"prod"}}`, egress,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				e, err := c.Egresses.Create(ctx, kibotos.CreateEgressRequest{
					Name: "egress", Zone: "global-1", Labels: filters.Labels, Protocol: kibotos.EgressProtocolSocks5, UDPEnabled: true,
					Socks5: kibotos.EgressSocks5Input{Address: "host:1080", Username: kibotos.Ptr("agent"), Password: kibotos.Ptr("input-only"),
						TLS: &kibotos.EgressTLSInput{ServerName: kibotos.Ptr("host")}},
				})
				if err == nil {
					data, _ := json.Marshal(e)
					if e.Revision != 2 || strings.Contains(string(data), "password") {
						t.Errorf("unexpected egress response %s", data)
					}
				}
				return e, err
			}},
		{"list egresses", "GET", "/v1/egresses", "label.env=prod&limit=5&page_token=next&zone=global-1", "", `{"data":[]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) { return c.Egresses.List(ctx, filters) }},
		{"get egress", "GET", "/v1/egresses/egr_1", "", "", egress,
			func(ctx context.Context, c *kibotos.Client) (any, error) { return c.Egresses.Get(ctx, "egr_1") }},
		{"replace egress", "PUT", "/v1/egresses/egr_1", "", `{"protocol":"socks5","socks5":{"address":"new:1080"},"udp_enabled":false,"drain_seconds":0}`, egress,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Egresses.Replace(ctx, "egr_1", kibotos.ReplaceEgressRequest{Protocol: kibotos.EgressProtocolSocks5, Socks5: kibotos.EgressSocks5Input{Address: "new:1080"}, DrainSeconds: kibotos.Ptr(0)})
			}},
		{"update egress", "PATCH", "/v1/egresses/egr_1", "", `{"name":null,"labels":{"remove":null}}`, egress,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Egresses.Update(ctx, "egr_1", kibotos.UpdateEgressRequest{Name: kibotos.Null[string](), Labels: map[string]*string{"remove": nil}})
			}},
		{"delete egress", "DELETE", "/v1/egresses/egr_1", "", "", "",
			func(ctx context.Context, c *kibotos.Client) (any, error) { return nil, c.Egresses.Delete(ctx, "egr_1") }},
		{"create resolver", "POST", "/v1/resolvers", "",
			`{"name":"dns","zone":"global-1","protocol":"udp","udp":{"address":"10.0.0.53:53"},"labels":{"env":"prod"}}`, resolver,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Resolvers.Create(ctx, kibotos.CreateResolverRequest{
					Name: "dns", Zone: "global-1", Labels: filters.Labels,
					Protocol: kibotos.ResolverProtocolUDP, UDP: &kibotos.ResolverAddress{Address: "10.0.0.53:53"},
				})
			}},
		{"list resolvers", "GET", "/v1/resolvers", "label.env=prod&limit=5&page_token=next&zone=global-1", "", `{"data":[]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) { return c.Resolvers.List(ctx, filters) }},
		{"get resolver", "GET", "/v1/resolvers/rsv_1", "", "", resolver,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				r, err := c.Resolvers.Get(ctx, "rsv_1")
				if err == nil && (r.HTTPS == nil || r.HTTPS.URL != "https://dns.example/dns-query" || r.Revision != 3) {
					t.Errorf("unexpected resolver: %+v", r)
				}
				return r, err
			}},
		{"replace resolver", "PUT", "/v1/resolvers/rsv_1", "", `{"protocol":"tls","tls":{"address":"dns.example:853"}}`, resolver,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Resolvers.Replace(ctx, "rsv_1", kibotos.ReplaceResolverRequest{Protocol: kibotos.ResolverProtocolTLS, TLS: &kibotos.ResolverAddress{Address: "dns.example:853"}})
			}},
		{"update resolver", "PATCH", "/v1/resolvers/rsv_1", "", `{"name":"updated","labels":{"env":"test"}}`, resolver,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Resolvers.Update(ctx, "rsv_1", kibotos.UpdateResolverRequest{Name: kibotos.Some("updated"), Labels: map[string]*string{"env": kibotos.Ptr("test")}})
			}},
		{"delete resolver", "DELETE", "/v1/resolvers/rsv_1", "", "", "",
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return nil, c.Resolvers.Delete(ctx, "rsv_1")
			}},
	})
}

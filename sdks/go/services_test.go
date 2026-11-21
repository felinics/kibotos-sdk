package kibotos_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

func TestZonesListAndIter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/zones" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("GET request should not auto-attach Idempotency-Key")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page_token") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"id":"global-1","display_name":"Global 1","region":"global","status":"available","default":true}],"next_page_token":"p2"}`))
		case "p2":
			_, _ = w.Write([]byte(`{"data":[{"id":"eu-1","display_name":"EU 1","region":"eu","status":"draining","default":false}],"next_page_token":null}`))
		default:
			t.Fatalf("unexpected page_token %q", r.URL.Query().Get("page_token"))
		}
	}))
	defer srv.Close()

	c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(srv.URL))
	var ids []string
	for z, err := range c.Zones.Iter(context.Background(), &kibotos.ZoneListParams{Limit: 1}) {
		if err != nil {
			t.Fatalf("Zones.Iter: %v", err)
		}
		ids = append(ids, z.ID)
	}
	want := []string{"global-1", "eu-1"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("got zone IDs %v, want %v", ids, want)
	}

	// Early break should stop before fetching the second page.
	var pageCalls atomic.Int32
	srvEarly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"global-1"},{"id":"global-2"}],"next_page_token":"p2"}`))
	}))
	defer srvEarly.Close()

	cEarly, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(srvEarly.URL))
	var first string
	for z, err := range cEarly.Zones.Iter(context.Background(), nil) {
		if err != nil {
			t.Fatalf("early Iter: %v", err)
		}
		first = z.ID
		break
	}
	if first != "global-1" || pageCalls.Load() != 1 {
		t.Fatalf("first=%q pageCalls=%d, want global-1 / 1", first, pageCalls.Load())
	}
}

func TestUpdateParamsPartialSerialization(t *testing.T) {
	b1, err := json.Marshal(kibotos.UpdateWorkspaceParams{
		Name: kibotos.Some("new-name"),
	})
	if err != nil {
		t.Fatalf("marshal b1: %v", err)
	}
	if string(b1) != `{"name":"new-name"}` {
		t.Fatalf("b1 = %s, want {\"name\":\"new-name\"}", string(b1))
	}

	b2, err := json.Marshal(kibotos.UpdateVolumeParams{
		Labels: map[string]*string{"env": nil},
	})
	if err != nil {
		t.Fatalf("marshal b2: %v", err)
	}
	if string(b2) != `{"labels":{"env":null}}` {
		t.Fatalf("b2 = %s, want {\"labels\":{\"env\":null}}", string(b2))
	}
}

func TestTemplatesAllEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/templates":
			if r.URL.Query().Get("owner") != "platform" || r.URL.Query().Get("zone") != "global-1" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{
				"data": [{
					"id": "python-3.12",
					"owner": "platform",
					"display_name": "Python 3.12",
					"description": "Python 3.12 Linux environment",
					"default_revision": "r42",
					"latest_revision": {"id": "r43", "status": "ready"},
					"zones": ["global-1"],
					"created_at": null,
					"updated_at": null
				}],
				"unavailable_zones": ["ap-1"],
				"next_page_token": null
			}`))
		case "/v1/templates/python-3.12":
			_, _ = w.Write([]byte(`{"id":"python-3.12","owner":"platform","default_revision":"r42","zones":["global-1"]}`))
		case "/v1/templates/python-3.12/revisions":
			_, _ = w.Write([]byte(`{"data":[{"id":"r42","template_id":"python-3.12","status":"ready","zones":["global-1"]}],"next_page_token":null}`))
		case "/v1/templates/python-3.12/revisions/r42":
			_, _ = w.Write([]byte(`{"id":"r42","template_id":"python-3.12","status":"ready","size_mib":1024,"zones":["global-1"]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(srv.URL))
	ctx := context.Background()

	page, err := c.Templates.List(ctx, &kibotos.TemplateListParams{
		Owner: kibotos.TemplateOwnerPlatform,
		Zone:  "global-1",
	})
	if err != nil {
		t.Fatalf("Templates.List: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != "python-3.12" || len(page.UnavailableZones) != 1 || page.UnavailableZones[0] != "ap-1" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.Data[0].LatestRevision == nil || page.Data[0].LatestRevision.ID != "r43" {
		t.Fatalf("unexpected latest_revision: %+v", page.Data[0].LatestRevision)
	}

	var iterCount int
	for tpl, err := range c.Templates.Iter(ctx, &kibotos.TemplateListParams{Owner: kibotos.TemplateOwnerPlatform, Zone: "global-1"}) {
		if err != nil {
			t.Fatalf("Templates.Iter: %v", err)
		}
		if tpl.ID == "python-3.12" {
			iterCount++
		}
	}
	if iterCount != 1 {
		t.Fatalf("iterCount = %d, want 1", iterCount)
	}

	tpl, err := c.Templates.Get(ctx, "python-3.12")
	if err != nil || tpl.DefaultRevision != "r42" {
		t.Fatalf("Templates.Get: %v, %+v", err, tpl)
	}

	revs, err := c.Templates.ListRevisions(ctx, "python-3.12", &kibotos.TemplateRevisionListParams{Limit: 10})
	if err != nil || len(revs.Data) != 1 || revs.Data[0].ID != "r42" {
		t.Fatalf("Templates.ListRevisions: %v, %+v", err, revs)
	}

	rev, err := c.Templates.GetRevision(ctx, "python-3.12", "r42")
	if err != nil || rev.SizeMiB == nil || *rev.SizeMiB != 1024 {
		t.Fatalf("Templates.GetRevision: %v, %+v", err, rev)
	}
}

func TestWorkspacesAllEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			if body["name"] != "agent-runner" {
				t.Errorf("unexpected name: %v", body["name"])
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"wks_1","name":"agent-runner","zone":"global-1","template":{"id":"python-3.12","revision":"r42"},"status":"creating","network":{"mode":"public_internet","applied":true}}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces":
			q := r.URL.Query()
			if q.Get("status") != "running" || q.Get("zone") != "global-1" || q.Get("label.team") != "ml" {
				t.Errorf("unexpected list query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"wks_1","status":"running"}],"unavailable_zones":[],"next_page_token":null}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_1":
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"running"}`))

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/workspaces/wks_1":
			var raw map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Fatalf("decode patch: %v", err)
			}
			if string(raw["name"]) != "null" {
				t.Errorf("expected explicit name:null, got %s", string(raw["name"]))
			}
			var labels map[string]*string
			if err := json.Unmarshal(raw["labels"], &labels); err != nil {
				t.Fatalf("decode labels: %v", err)
			}
			if labels["keep"] == nil || *labels["keep"] != "yes" {
				t.Errorf("expected keep=yes, got %+v", labels)
			}
			if v, ok := labels["del_me"]; !ok || v != nil {
				t.Errorf("expected del_me=null, got ok=%v v=%v", ok, v)
			}
			_, _ = w.Write([]byte(`{"id":"wks_1","name":"","status":"running","labels":{"keep":"yes"}}`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_1/start":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"starting"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_1/stop":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["force"] != true {
				t.Errorf("expected force=true, got %v", body)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"stopping"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_1/timeout":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["seconds"] != float64(1800) || body["action"] != "stop" {
				t.Errorf("unexpected timeout body: %v", body)
			}
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"running","timeout":{"expires_at":"2026-11-21T13:00:00Z","action":"stop"}}`))

		case r.Method == http.MethodPut && r.URL.Path == "/v1/workspaces/wks_1/network":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"running","network":{"mode":"deny_all","applied":false}}`))

		case r.Method == http.MethodPut && r.URL.Path == "/v1/workspaces/wks_1/volume":
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"stopped","volume":{"id":"vol_1","mount_path":"/data","delete_with_workspace":false}}`))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_1/volume":
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"stopped","volume":null}`))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_1":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"wks_1","status":"deleting"}`))

		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(srv.URL))
	ctx := context.Background()

	ws, err := c.Workspaces.Create(ctx, kibotos.CreateWorkspaceParams{
		Name:     "agent-runner",
		Zone:     "global-1",
		Template: kibotos.WorkspaceTemplateSpec{ID: "python-3.12", Revision: "r42"},
		Resources: &kibotos.WorkspaceResourcesSpec{
			VCPUs: 2, MemoryMiB: 4096, DiskMiB: 20480,
		},
		Network: &kibotos.WorkspaceNetworkSpec{Mode: kibotos.NetworkModePublicInternet},
		Timeout: &kibotos.TimeoutSpec{Seconds: 3600, Action: kibotos.TimeoutActionStop},
		Labels:  map[string]string{"team": "ml"},
	})
	if err != nil || ws.ID != "wks_1" {
		t.Fatalf("Create: %v, %+v", err, ws)
	}

	page, err := c.Workspaces.List(ctx, &kibotos.WorkspaceListParams{
		Status: kibotos.WorkspaceStatusRunning,
		Zone:   "global-1",
		Labels: map[string]string{"team": "ml"},
	})
	if err != nil || len(page.Data) != 1 {
		t.Fatalf("List: %v, %+v", err, page)
	}

	var iterIDs []string
	for item, err := range c.Workspaces.Iter(ctx, &kibotos.WorkspaceListParams{
		Status: kibotos.WorkspaceStatusRunning,
		Zone:   "global-1",
		Labels: map[string]string{"team": "ml"},
	}) {
		if err != nil {
			t.Fatalf("Iter: %v", err)
		}
		iterIDs = append(iterIDs, item.ID)
	}
	if len(iterIDs) != 1 || iterIDs[0] != "wks_1" {
		t.Fatalf("unexpected iterIDs: %v", iterIDs)
	}

	if _, err := c.Workspaces.Get(ctx, "wks_1"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	updated, err := c.Workspaces.Update(ctx, "wks_1", kibotos.UpdateWorkspaceParams{
		Name: kibotos.Null[string](),
		Labels: map[string]*string{
			"keep":   kibotos.Ptr("yes"),
			"del_me": nil,
		},
	})
	if err != nil || updated.Labels["keep"] != "yes" {
		t.Fatalf("Update: %v, %+v", err, updated)
	}

	if _, err := c.Workspaces.Start(ctx, "wks_1", &kibotos.StartWorkspaceParams{
		Timeout: &kibotos.TimeoutSpec{Seconds: 3600, Action: kibotos.TimeoutActionStop},
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := c.Workspaces.Stop(ctx, "wks_1", &kibotos.StopWorkspaceParams{Force: true}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := c.Workspaces.SetTimeout(ctx, "wks_1", kibotos.SetWorkspaceTimeoutParams{
		Seconds: 1800, Action: kibotos.TimeoutActionStop,
	}); err != nil {
		t.Fatalf("SetTimeout: %v", err)
	}
	if _, err := c.Workspaces.SetNetwork(ctx, "wks_1", kibotos.SetWorkspaceNetworkParams{
		Mode: kibotos.NetworkModeDenyAll,
	}); err != nil {
		t.Fatalf("SetNetwork: %v", err)
	}
	if _, err := c.Workspaces.AttachVolume(ctx, "wks_1", kibotos.AttachVolumeParams{
		ID: "vol_1", MountPath: "/data",
	}); err != nil {
		t.Fatalf("AttachVolume: %v", err)
	}
	if _, err := c.Workspaces.DetachVolume(ctx, "wks_1"); err != nil {
		t.Fatalf("DetachVolume: %v", err)
	}
	if _, err := c.Workspaces.Delete(ctx, "wks_1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestWorkspacesWaitChunksAndErrors(t *testing.T) {
	var waitCalls atomic.Int32
	var seenTimeouts []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/workspaces/wks_loop/wait":
			statuses := r.URL.Query()["status"]
			if !reflect.DeepEqual(statuses, []string{"running", "failed"}) {
				t.Errorf("unexpected statuses: %v", statuses)
			}
			seenTimeouts = append(seenTimeouts, r.URL.Query().Get("timeout"))
			n := waitCalls.Add(1)
			if n < 3 {
				w.WriteHeader(http.StatusRequestTimeout)
				_, _ = w.Write([]byte(`{"error":{"code":"wait_timeout","message":"still creating","status":408,"retryable":true}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"wks_loop","status":"running"}`))

		case "/v1/workspaces/wks_unexp/wait":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"unexpected_status","message":"workspace entered failed","status":409,"retryable":false}}`))

		case "/v1/workspaces/wks_timeout/wait":
			time.Sleep(40 * time.Millisecond)
			w.WriteHeader(http.StatusRequestTimeout)
			_, _ = w.Write([]byte(`{"error":{"code":"wait_timeout","message":"timed out","status":408,"retryable":true}}`))

		case "/v1/workspaces/wks_wait_err/wait":
			time.Sleep(25 * time.Millisecond)
			w.WriteHeader(http.StatusRequestTimeout)
			_, _ = w.Write([]byte(`{"error":{"code":"wait_timeout","message":"still creating","status":408,"retryable":true,"details":{"workspace":{"id":"wks_wait_err","status":"creating"}}}}`))
		}
	}))
	defer srv.Close()

	c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(srv.URL))
	ctx := context.Background()

	// 2-minute timeout -> server chunks capped at 60s, loops cleanly across 408 wait_timeout.
	ws, err := c.Workspaces.Wait(ctx, "wks_loop", kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusRunning, kibotos.WorkspaceStatusFailed},
		Timeout:  120 * time.Second,
	})
	if err != nil {
		t.Fatalf("Wait wks_loop: %v", err)
	}
	if ws.Status != kibotos.WorkspaceStatusRunning || waitCalls.Load() != 3 {
		t.Fatalf("got status=%q calls=%d", ws.Status, waitCalls.Load())
	}
	for _, to := range seenTimeouts {
		if to != "60" {
			t.Fatalf("expected 60s server chunk timeout, got %v", seenTimeouts)
		}
	}

	// unexpected_status returns immediately.
	_, err = c.Workspaces.Wait(ctx, "wks_unexp", kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusRunning},
		Timeout:  10 * time.Second,
	})
	if !kibotos.IsUnexpectedStatus(err) {
		t.Fatalf("expected IsUnexpectedStatus, got %v", err)
	}

	// Context deadline expires mid-request.
	_, err = c.Workspaces.Wait(ctx, "wks_timeout", kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusRunning},
		Timeout:  20 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	// Server returns 408 wait_timeout right as deadline expires -> IsWaitTimeout is true.
	_, err = c.Workspaces.Wait(ctx, "wks_wait_err", kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusRunning},
		Timeout:  30 * time.Millisecond,
	})
	if !kibotos.IsWaitTimeout(err) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected IsWaitTimeout or DeadlineExceeded, got %v", err)
	}
}

func TestVolumesAndExposuresAllEndpoints(t *testing.T) {
	var volWaitCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/volumes":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"vol_1","name":"datasets","zone":"global-1","size_gib":50,"status":"creating","workspace_id":null,"labels":{"env":"dev"}}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/volumes":
			q := r.URL.Query()
			if q.Get("attached") != "false" || q.Get("zone") != "global-1" || q.Get("label.env") != "dev" {
				t.Errorf("unexpected volumes query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"vol_1","size_gib":50,"status":"available"}],"unavailable_zones":[],"next_page_token":null}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/volumes/vol_1":
			_, _ = w.Write([]byte(`{"id":"vol_1","size_gib":50,"status":"available"}`))

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/volumes/vol_1":
			_, _ = w.Write([]byte(`{"id":"vol_1","name":"renamed","size_gib":50,"status":"available"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/volumes/vol_1/resize":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"vol_1","size_gib":100,"status":"resizing"}`))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/volumes/vol_1":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"vol_1","status":"deleting"}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/volumes/vol_1/wait":
			if volWaitCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusRequestTimeout)
				_, _ = w.Write([]byte(`{"error":{"code":"wait_timeout","message":"wait","status":408,"retryable":true}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"vol_1","size_gib":100,"status":"available"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_1/exposures":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"exp_1","workspace_id":"wks_1","port":8080,"url":"https://k7x2n4p6.kibo.run","status":"active","expires_at":"2026-11-21T13:00:00Z","revoked_at":null,"created_at":"2026-11-21T12:00:00Z"}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_1/exposures":
			if r.URL.Query().Get("include_revoked") != "true" {
				t.Errorf("expected include_revoked=true, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"exp_1","workspace_id":"wks_1","port":8080,"url":"https://k7x2n4p6.kibo.run","status":"active"}],"next_page_token":null}`))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_1/exposures/exp_1":
			_, _ = w.Write([]byte(`{"id":"exp_1","workspace_id":"wks_1","port":8080,"status":"active"}`))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_1/exposures/exp_1":
			_, _ = w.Write([]byte(`{"id":"exp_1","workspace_id":"wks_1","port":8080,"status":"revoked","revoked_at":"2026-11-21T12:15:00Z"}`))

		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(srv.URL))
	ctx := context.Background()

	vol, err := c.Volumes.Create(ctx, kibotos.CreateVolumeParams{
		Name: "datasets", Zone: "global-1", SizeGiB: 50, Labels: map[string]string{"env": "dev"},
	})
	if err != nil || vol.ID != "vol_1" {
		t.Fatalf("Volumes.Create: %v, %+v", err, vol)
	}

	var volCount int
	for item, err := range c.Volumes.Iter(ctx, &kibotos.VolumeListParams{
		Zone:     "global-1",
		Attached: kibotos.Ptr(false),
		Labels:   map[string]string{"env": "dev"},
	}) {
		if err != nil {
			t.Fatalf("Volumes.Iter: %v", err)
		}
		if item.ID == "vol_1" {
			volCount++
		}
	}
	if volCount != 1 {
		t.Fatalf("volCount = %d, want 1", volCount)
	}

	if _, err := c.Volumes.Get(ctx, "vol_1"); err != nil {
		t.Fatalf("Volumes.Get: %v", err)
	}
	if _, err := c.Volumes.Update(ctx, "vol_1", kibotos.UpdateVolumeParams{Name: kibotos.Some("renamed")}); err != nil {
		t.Fatalf("Volumes.Update: %v", err)
	}
	if _, err := c.Volumes.Resize(ctx, "vol_1", kibotos.ResizeVolumeParams{SizeGiB: 100}); err != nil {
		t.Fatalf("Volumes.Resize: %v", err)
	}
	waited, err := c.Volumes.Wait(ctx, "vol_1", kibotos.WaitVolumeParams{
		Statuses: []kibotos.VolumeStatus{kibotos.VolumeStatusAvailable},
		Timeout:  90 * time.Second,
	})
	if err != nil || waited.Status != kibotos.VolumeStatusAvailable || volWaitCalls.Load() != 2 {
		t.Fatalf("Volumes.Wait: err=%v vol=%+v calls=%d", err, waited, volWaitCalls.Load())
	}
	if _, err := c.Volumes.Delete(ctx, "vol_1"); err != nil {
		t.Fatalf("Volumes.Delete: %v", err)
	}

	exp, err := c.Exposures.Create(ctx, "wks_1", kibotos.CreateExposureParams{Port: 8080, TTLSeconds: 3600})
	if err != nil || exp.URL != "https://k7x2n4p6.kibo.run" {
		t.Fatalf("Exposures.Create: %v, %+v", err, exp)
	}
	var expCount int
	for item, err := range c.Exposures.Iter(ctx, "wks_1", &kibotos.ExposureListParams{IncludeRevoked: true}) {
		if err != nil {
			t.Fatalf("Exposures.Iter: %v", err)
		}
		if item.ID == "exp_1" {
			expCount++
		}
	}
	if expCount != 1 {
		t.Fatalf("expCount = %d, want 1", expCount)
	}
	if _, err := c.Exposures.Get(ctx, "wks_1", "exp_1"); err != nil {
		t.Fatalf("Exposures.Get: %v", err)
	}
	revoked, err := c.Exposures.Revoke(ctx, "wks_1", "exp_1")
	if err != nil || revoked.Status != kibotos.ExposureStatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("Exposures.Revoke: %v, %+v", err, revoked)
	}
}

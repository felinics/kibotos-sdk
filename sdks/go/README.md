# Kibotos Go SDK

Official Go SDK for the Kibotos v1 API (Go 1.24+). WebSocket support uses `github.com/coder/websocket`.

## Installation

```bash
go get github.com/felinics/kibotos-sdk/sdks/go
```

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

func main() {
	ctx := context.Background()

	// Reads KIBOTOS_API_KEY and optional KIBOTOS_API_URL.
	client, err := kibotos.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	// Create a workspace and wait for it to become available.
	ws, err := client.Workspaces.Create(ctx, kibotos.CreateWorkspaceParams{
		Name:     "agent-runner",
		Template: kibotos.WorkspaceTemplateSpec{ID: "python-3.12"},
		Resources: &kibotos.WorkspaceResourcesSpec{
			VCPUs:     2,
			MemoryMiB: 4096,
		},
		Timeout: &kibotos.TimeoutSpec{
			Seconds: 3600,
			Action:  kibotos.TimeoutActionStop,
		},
		Labels: map[string]string{"team": "ml"},
	})
	if err != nil {
		log.Fatal(err)
	}

	ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusAvailable},
		Timeout:  3 * time.Minute,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Workspace %s is %s in zone %s\n", ws.ID, ws.Status, ws.Zone)

	// Expose port 8080 over HTTPS.
	exp, err := client.Exposures.Create(ctx, ws.ID, kibotos.CreateExposureParams{
		Port:       8080,
		TTLSeconds: 1800,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Exposure URL: %s\n", exp.URL)
}
```

## Client Configuration

```go
client, err := kibotos.NewClient(
	"kb_...",
	kibotos.WithBaseURL("https://api.kibo.to/v1"),
	kibotos.WithTimeout(30*time.Second),
	kibotos.WithMaxRetries(3),
	kibotos.WithUserAgent("my-service/1.0"),
)
```

## Pagination Iterators

Zones, Templates, Workspaces, Volumes, and Exposures provide an `Iter` method returning `iter.Seq2[T, error]`. Use `List` and the returned `NextPageToken` for Packs, Egresses, and Resolvers.

```go
for ws, err := range client.Workspaces.Iter(ctx, &kibotos.WorkspaceListParams{
	Status: kibotos.WorkspaceStatusAvailable,
	Labels: map[string]string{"team": "ml"},
}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(ws.ID, ws.Name)
}
```

## Idempotency, Retries & Error Handling

- Control-plane `POST` mutations and egress and resolver replacements attach a UUID v4 `Idempotency-Key` and reuse it across retry attempts. Override the key with `kibotos.WithIdempotencyKey("...")`.
- On retry-enabled requests, retryable API errors (`retryable: true`), HTTP `429` (honoring `Retry-After`), `500`/`502`/`503`/`504`, and transient network errors are retried with exponential backoff and jitter.
- Process execution, file transfers, file mutations, and `Proxy` send one request and never carry an `Idempotency-Key`. File content transfers and SSE use the caller’s context and any timeout configured on a custom HTTP client. Set a deadline on the caller’s context to bound the entire operation.
- Inspect errors with `errors.As(err, &apiErr)` or helpers `kibotos.IsNotFound(err)`, `kibotos.IsRetryable(err)`, `kibotos.IsRateLimited(err)`, `kibotos.IsWaitTimeout(err)`, `kibotos.IsUnexpectedStatus(err)`, and `kibotos.IsUnauthenticated(err)`.

## Services Covered

- `client.Zones`: `List`, `Iter`
- `client.Templates`: `List`, `Iter`, `Get`, `ListRevisions`, `GetRevision`
- `client.Workspaces`: `Create`, `List`, `Iter`, `Get`, `Update`, `Delete`, `Start`, `Stop`, `SetTimeout`, `SetNetwork`, `AttachVolume`, `DetachVolume`, `SetVolumes`, `SetPacks`, `GetMetrics`, `Wait`, `Exec`, `ExecStream`, `ListFiles`, `StatFile`, `ReadFile`, `WriteFile`, `MakeDir`, `MoveFile`, `DeleteFile`, `Proxy`, `DialWS`, `DialTunnel`, `DialDesktopVNC`
- `client.Volumes`: `Create`, `List`, `Iter`, `Get`, `Update`, `Resize`, `Delete`, `Wait`
- `client.Exposures`: `Create`, `List`, `Iter`, `Get`, `Revoke`
- `client.Packs` provides `List`, `Get`, and `GetRevision`.
- `client.Egresses` and `client.Resolvers` each provide `Create`, `List`, `Get`, `Replace`, `Update`, and `Delete`. `Replace` sends the complete protocol configuration. `Update` changes display names and labels.

## Workspace configuration

Create workspaces with `CreateWorkspaceRequest.Volumes` and `Packs`. The legacy `Volume` field remains available for single-volume callers. `AttachVolume` leaves only the given volume attached and `DetachVolume` removes every attachment, so use `SetVolumes` on multi-volume workspaces. Resize stopped or failed workspaces with `UpdateWorkspaceRequest.Resources`. `SetVolumes` and `SetPacks` accept empty slices to clear the corresponding bindings. A workspace binds up to four Packs, and Pack changes apply on the next start. `SetNetwork` takes the complete `WorkspaceNetworkInput`, including `Allow`, `Deny`, `Egresses`, and `Resolvers`. `DrainSeconds: kibotos.Ptr(0)` requests zero drain time. The response field `Network.Applied` is a `*bool` and is nil while the workspace is not running.

## Workspace data plane

```go
result, err := client.Workspaces.Exec(ctx, workspaceID, kibotos.ExecRequest{
    Argv: []string{"python3", "--version"},
})
if err != nil {
    return err
}
fmt.Print(result.Stdout) // Inspect result.ExitCode for the remote status.

file, err := client.Workspaces.ReadFile(ctx, workspaceID, "/app/output.bin",
    kibotos.ReadFileOptions{Range: "bytes=0-1023"})
if err != nil {
    return err
}
defer file.Close()
_, err = io.Copy(dst, file)
```

`ExecStream` calls a handler for each SSE event, including `started`, output, heartbeats and the terminal `exited` or `error` event. The handler can return an error to stop processing. `ReadFile` exposes the HTTP status and response headers alongside the stream. `WriteFile` takes an `io.Reader` and supports mode, user and create-only options.

`DialWS` opens the multiplexed channel. Use `OpenProcess` with `start` or `attach`, `OpenFSWatch`, and `OpenTunnel` with distinct positive stream IDs. `Read` yields either JSON control events or binary frames. `WriteControl` sends controls such as PTY resize. `WriteBinary` encodes the version byte, four-byte big-endian stream ID, and one-byte channel before the payload. Keep a reader running to process WebSocket pings. `DialTunnel` takes a guest TCP port or Unix path. It and `DialDesktopVNC` exchange raw binary messages with no multiplexing header.

`Proxy` forwards an authenticated HTTP request to a loopback port inside the workspace and returns the raw `*http.Response` for every status.

## Documentation

Reference guides live in [`docs/`](./docs) and are published at [docs.kibo.to](https://docs.kibo.to): workspaces, volumes, exposures, exec / files / tunnels, Packs and networking, templates and zones, and errors and retries.

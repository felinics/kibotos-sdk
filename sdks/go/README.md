# Kibotos Go SDK

Official zero-dependency Go SDK for the Kibotos v1 API (Go 1.24+).

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

	// Create a workspace and wait for it to become running.
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
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusRunning},
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

Every listable service provides an `Iter` method returning `iter.Seq2[T, error]`:

```go
for ws, err := range client.Workspaces.Iter(ctx, &kibotos.WorkspaceListParams{
	Status: kibotos.WorkspaceStatusRunning,
	Labels: map[string]string{"team": "ml"},
}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(ws.ID, ws.Name)
}
```

## Idempotency, Retries & Error Handling

- Every `POST` mutation automatically attaches a UUID v4 `Idempotency-Key` (override with `kibotos.WithIdempotencyKey("...")`) and reuses the same key across retry attempts.
- Retryable API errors (`retryable: true`), HTTP `429` (honoring `Retry-After`), `500`/`502`/`503`/`504`, and transient network errors are retried with exponential backoff and jitter.
- Inspect errors with `errors.As(err, &apiErr)` or helpers `kibotos.IsNotFound(err)`, `kibotos.IsRetryable(err)`, `kibotos.IsRateLimited(err)`, `kibotos.IsWaitTimeout(err)`, and `kibotos.IsUnauthenticated(err)`.

## Services Covered

- `client.Zones`: `List`, `Iter`
- `client.Templates`: `List`, `Iter`, `Get`, `ListRevisions`, `GetRevision`
- `client.Workspaces`: `Create`, `List`, `Iter`, `Get`, `Update`, `Delete`, `Start`, `Stop`, `SetTimeout`, `SetNetwork`, `AttachVolume`, `DetachVolume`, `Wait`
- `client.Volumes`: `Create`, `List`, `Iter`, `Get`, `Update`, `Resize`, `Delete`, `Wait`
- `client.Exposures`: `Create`, `List`, `Iter`, `Get`, `Revoke`

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

func main() {
	ctx := context.Background()

	// Initialize client from KIBOTOS_API_KEY (and optional KIBOTOS_API_URL).
	client, err := kibotos.NewClientFromEnv()
	if err != nil {
		log.Fatalf("init client: %v", err)
	}

	// 1. List available zones.
	fmt.Println("Available zones:")
	for zone, err := range client.Zones.Iter(ctx, nil) {
		if err != nil {
			log.Fatalf("list zones: %v", err)
		}
		fmt.Printf("  - %s (%s, status=%s, default=%v)\n", zone.ID, zone.DisplayName, zone.Status, zone.Default)
	}

	// 2. List platform templates.
	fmt.Println("\nPlatform templates:")
	for tpl, err := range client.Templates.Iter(ctx, &kibotos.TemplateListParams{
		Owner: kibotos.TemplateOwnerPlatform,
	}) {
		if err != nil {
			log.Fatalf("list templates: %v", err)
		}
		fmt.Printf("  - %s (%s, default_revision=%s)\n", tpl.ID, tpl.DisplayName, tpl.DefaultRevision)
	}

	// 3. Create a workspace and wait until it is running.
	ws, err := client.Workspaces.Create(ctx, kibotos.CreateWorkspaceParams{
		Name:     "go-sdk-example",
		Template: kibotos.WorkspaceTemplateSpec{ID: "python-3.12"},
		Resources: &kibotos.WorkspaceResourcesSpec{
			VCPUs:     2,
			MemoryMiB: 4096,
		},
		Network: &kibotos.WorkspaceNetworkSpec{
			Mode: kibotos.NetworkModePublicInternet,
		},
		Timeout: &kibotos.TimeoutSpec{
			Seconds: 1800,
			Action:  kibotos.TimeoutActionStop,
		},
		Labels: map[string]string{
			"example": "go-sdk",
		},
	})
	if err != nil {
		log.Fatalf("create workspace: %v", err)
	}
	fmt.Printf("\nCreated workspace %s (status=%s), waiting for running...\n", ws.ID, ws.Status)

	ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusRunning},
		Timeout:  3 * time.Minute,
	})
	if err != nil {
		var apiErr *kibotos.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("wait workspace failed (%s, request_id=%s): %s", apiErr.Code, apiErr.RequestID, apiErr.Message)
		}
		log.Fatalf("wait workspace: %v", err)
	}
	fmt.Printf("Workspace %s is %s in zone %s\n", ws.ID, ws.Status, ws.Zone)

	// 4. Create an HTTPS port exposure and then revoke it.
	exp, err := client.Exposures.Create(ctx, ws.ID, kibotos.CreateExposureParams{
		Port:       8080,
		TTLSeconds: 600,
	})
	if err != nil {
		log.Fatalf("create exposure: %v", err)
	}
	fmt.Printf("Exposed port %d at %s (id=%s)\n", exp.Port, exp.URL, exp.ID)

	if _, err := client.Exposures.Revoke(ctx, ws.ID, exp.ID); err != nil {
		log.Fatalf("revoke exposure: %v", err)
	}
	fmt.Printf("Revoked exposure %s\n", exp.ID)

	// 5. Clean up the workspace.
	if _, err := client.Workspaces.Delete(ctx, ws.ID); err != nil {
		log.Fatalf("delete workspace: %v", err)
	}
	fmt.Printf("Requested deletion of workspace %s\n", ws.ID)
}

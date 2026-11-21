package cli

import (
	"runtime"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

type versionInfo struct {
	Version    string `json:"version"`
	SDKVersion string `json:"sdk_version"`
	Commit     string `json:"commit,omitempty"`
	Date       string `json:"date,omitempty"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
}

func (a *App) newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print kibo CLI version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := versionInfo{
				Version:    a.build.Version,
				SDKVersion: kibotos.Version,
				Commit:     a.build.Commit,
				Date:       a.build.Date,
				GoVersion:  runtime.Version(),
				OS:         runtime.GOOS,
				Arch:       runtime.GOARCH,
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(info)
			}
			items := []output.KV{
				{Key: "CLI Version", Value: info.Version},
				{Key: "SDK Version", Value: info.SDKVersion},
				{Key: "Go Version", Value: info.GoVersion},
				{Key: "Platform", Value: info.OS + "/" + info.Arch},
			}
			if info.Commit != "" {
				items = append(items, output.KV{Key: "Commit", Value: info.Commit})
			}
			if info.Date != "" {
				items = append(items, output.KV{Key: "Built", Value: info.Date})
			}
			return p.Details(items)
		},
	}
}

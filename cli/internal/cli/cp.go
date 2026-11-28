package cli

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

const copyExample = "Use exactly one workspace:path operand, for example `kibo cp ./report.txt dev:/data/` or `kibo cp dev:/data/report.txt .`."

func (a *App) newCopyCmd() *cobra.Command {
	var write kibotos.WriteFileOptions
	cmd := &cobra.Command{
		Use:     "cp <src> <dst>",
		Short:   "Copy a file between your local machine and a workspace",
		Example: "  kibo cp ./report.txt dev:/data/\n  kibo cp dev:/data/report.txt -",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcRef, srcPath, srcRemote := splitCopyOperand(args[0])
			dstRef, dstPath, dstRemote := splitCopyOperand(args[1])
			if srcRemote == dstRemote {
				return usageErrorWithHint(copyExample, "cp requires one local path and one workspace:path")
			}
			ref, remote, local := dstRef, dstPath, srcPath
			if srcRemote {
				ref, remote, local = srcRef, srcPath, dstPath
			}
			if strings.TrimSpace(ref) == "" || strings.TrimSpace(remote) == "" || local == "" {
				return usageErrorWithHint(copyExample, "cp requires both a workspace reference and nonempty paths")
			}
			if err := validateNonEmptyFlag(cmd, "user", write.User); err != nil {
				return err
			}
			if err := validateFileMode(write.Mode); err != nil {
				return err
			}
			if srcRemote {
				if cmd.Flags().Changed("mode") || cmd.Flags().Changed("create-only") {
					return usageErrorWithHint("Use --mode or --create-only when copying to a workspace.", "--mode and --create-only are upload options")
				}
				if local == "-" && a.printer().IsJSON() {
					return usageErrorWithHint("Pass a local destination file to use --json.", "cp cannot combine JSON output with a stdout download")
				}
				if info, err := os.Stat(local); strings.HasSuffix(local, "/") || strings.HasSuffix(local, string(filepath.Separator)) || (err == nil && info.IsDir()) {
					local = filepath.Join(local, path.Base(remote))
				}
			} else {
				if local == "-" {
					return usageErrorWithHint("Pass a local file or use `kibo files put` to upload from stdin.", "cp uploads require a local file")
				}
				if remote == "." || strings.HasSuffix(remote, "/") {
					remote = path.Join(remote, filepath.Base(local))
				}
			}
			ctx := cmd.Context()
			client, id, err := a.resolveAvailableWorkspace(ctx, ref)
			if err != nil {
				return err
			}
			if srcRemote {
				return a.downloadFile(ctx, client.Workspaces, id, remote, local, kibotos.ReadFileOptions{User: write.User})
			}
			return a.uploadFile(ctx, client.Workspaces, id, local, remote, write)
		},
	}
	f := cmd.Flags()
	f.StringVar(&write.User, "user", "", "Workspace user for permissions and relative paths")
	f.StringVar(&write.Mode, "mode", "", "Octal permissions for uploads, for example 0644")
	f.BoolVar(&write.CreateOnly, "create-only", false, "Fail if the upload destination exists")
	return cmd
}

func splitCopyOperand(operand string) (ref, file string, remote bool) {
	prefix, suffix, found := strings.Cut(operand, ":")
	if !found || strings.ContainsAny(prefix, "/\\") {
		return "", operand, false
	}
	if len(prefix) == 1 && (prefix[0] >= 'A' && prefix[0] <= 'Z' || prefix[0] >= 'a' && prefix[0] <= 'z') &&
		(strings.HasPrefix(suffix, "/") || strings.HasPrefix(suffix, "\\")) {
		return "", operand, false
	}
	return prefix, suffix, true
}

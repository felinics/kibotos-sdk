package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) newFilesCmd() *cobra.Command {
	root := &cobra.Command{Use: "files", Short: "Browse and transfer workspace files"}
	var user string
	root.PersistentFlags().StringVar(&user, "user", "", "Workspace user for permissions and relative paths")

	var depth int
	list := a.fileCmd("ls <workspace> [path]", "List a workspace directory", cobra.RangeArgs(1, 2), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		if depth < 1 || uint64(depth) > 1<<32-1 {
			return usageError("--depth must be between 1 and 4294967295")
		}
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		result, err := service.ListFiles(ctx, id, path, kibotos.ListFilesOptions{User: user, Depth: depth})
		if err != nil {
			return err
		}
		p := a.printer()
		if p.IsJSON() {
			return p.JSON(result)
		}
		if p.IsQuiet() {
			for _, entry := range result.Entries {
				if err := p.ID(entry.Path); err != nil {
					return err
				}
			}
			return nil
		}
		rows := make([][]string, 0, len(result.Entries))
		for _, entry := range result.Entries {
			rows = append(rows, []string{entry.Path, entry.Type, entry.Mode, fmt.Sprint(entry.SizeBytes), optionalFileTime(entry.ModifiedAt)})
		}
		return p.Table([]string{"PATH", "TYPE", "MODE", "BYTES", "MODIFIED"}, rows)
	})
	list.Flags().IntVar(&depth, "depth", 1, "Number of directory levels to include")
	stat := a.fileCmd("stat <workspace> <path>", "Show file or directory attributes", cobra.ExactArgs(2), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		entry, err := service.StatFile(ctx, id, args[0], kibotos.FileOptions{User: user})
		if err != nil {
			return err
		}
		return a.renderFile(entry)
	})

	var byteRange string
	get := a.fileCmd("get <workspace> <path> [local-file|-]", "Download a file (defaults to stdout)", cobra.RangeArgs(2, 3), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		p := a.printer()
		localFile := len(args) == 2 && args[1] != "-"
		if p.IsJSON() && !localFile {
			return usageError("files get --json requires a local destination file")
		}
		body, err := service.ReadFile(ctx, id, args[0], kibotos.ReadFileOptions{User: user, Range: byteRange})
		if err != nil {
			return err
		}
		defer body.Close()
		if !localFile {
			_, err = io.Copy(p.Out, body)
			return err
		}
		written, err := saveDownload(args[1], body)
		if err != nil {
			return err
		}
		if p.IsJSON() {
			return p.JSON(map[string]any{
				"path": args[0], "local_path": args[1], "size_bytes": written,
				"etag": body.Header.Get("ETag"), "content_range": body.Header.Get("Content-Range"),
			})
		}
		return p.ID(args[1])
	})
	get.Flags().StringVar(&byteRange, "range", "", "Byte range, for example bytes=0-1023")

	var write kibotos.WriteFileOptions
	put := a.fileCmd("put <workspace> <local-file|-> <path>", "Upload a file from disk or stdin", cobra.ExactArgs(3), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		input := struct{ io.Reader }{a.stdin}
		if args[0] != "-" {
			file, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() && info.Size() > 1<<30 {
				return usageError("local file %q exceeds the 1 GiB upload limit", args[0])
			}
			input.Reader = file
		}
		write.User = user
		result, err := service.WriteFile(ctx, id, args[1], input, write)
		if err != nil {
			return err
		}
		p := a.printer()
		if p.IsJSON() {
			return p.JSON(result)
		}
		if p.IsQuiet() {
			return p.ID(result.Path)
		}
		return p.Details([]output.KV{{Key: "Path", Value: result.Path}, {Key: "Bytes", Value: fmt.Sprint(result.SizeBytes)}, {Key: "SHA-256", Value: result.SHA256}})
	})
	put.Flags().StringVar(&write.Mode, "mode", "", "Octal permissions, for example 0644")
	put.Flags().BoolVar(&write.CreateOnly, "create-only", false, "Fail if the destination exists")

	var dirMode string
	mkdir := a.fileCmd("mkdir <workspace> <path>", "Create a workspace directory", cobra.ExactArgs(2), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		entry, err := service.MakeDir(ctx, id, kibotos.MakeDirectoryRequest{Path: args[0], Mode: dirMode, User: user})
		if err != nil {
			return err
		}
		return a.renderFile(entry)
	})
	mkdir.Flags().StringVar(&dirMode, "mode", "", "Octal permissions, for example 0755")
	mv := a.fileCmd("mv <workspace> <source> <destination>", "Move or rename a workspace file", cobra.ExactArgs(3), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		entry, err := service.MoveFile(ctx, id, kibotos.MoveFileRequest{Source: args[0], Destination: args[1], User: user})
		if err != nil {
			return err
		}
		return a.renderFile(entry)
	})
	rm := a.fileCmd("rm <workspace> <path>", "Delete a workspace file or directory", cobra.ExactArgs(2), func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		ok, err := a.confirmDestructive(fmt.Sprintf("Delete %q from %s?", args[0], id))
		if err != nil {
			return err
		}
		if !ok {
			return prompt.ErrCancelled
		}
		if err := service.DeleteFile(ctx, id, args[0], kibotos.FileOptions{User: user}); err != nil {
			return err
		}
		p := a.printer()
		if p.IsJSON() {
			return p.JSON(map[string]any{"path": args[0], "deleted": true})
		}
		return p.ID(args[0])
	})
	root.AddCommand(list, stat, get, put, mkdir, mv, rm)
	return root
}

func (a *App) fileCmd(use, short string, count cobra.PositionalArgs, run func(context.Context, *kibotos.WorkspacesService, string, []string) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: count, RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		client, id, err := a.workspaceDataClient(ctx, args[0])
		if err != nil {
			return err
		}
		return run(ctx, client.Workspaces, id, args[1:])
	}}
}

func (a *App) renderFile(entry *kibotos.FileEntry) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(entry)
	}
	if p.IsQuiet() {
		return p.ID(entry.Path)
	}
	return p.Details([]output.KV{
		{Key: "Path", Value: entry.Path}, {Key: "Type", Value: entry.Type},
		{Key: "Mode", Value: entry.Mode}, {Key: "Bytes", Value: fmt.Sprint(entry.SizeBytes)},
		{Key: "Modified", Value: optionalFileTime(entry.ModifiedAt)},
	})
}

func optionalFileTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return output.FormatTime(*t)
}

func saveDownload(path string, input io.Reader) (int64, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".kibo-download-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(file.Name())
	n, err := io.Copy(file, input)
	if err == nil {
		err = file.Chmod(0644)
	}
	closeErr := file.Close()
	if err != nil {
		return n, err
	}
	if closeErr != nil {
		return n, closeErr
	}
	return n, os.Rename(file.Name(), path)
}

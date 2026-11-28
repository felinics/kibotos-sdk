package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	list := a.fileCmd("ls [workspace] [path]", "List a workspace directory", 2, nil, func(_ []string) error {
		if depth < 1 || uint64(depth) > 1<<32-1 {
			return usageError("--depth must be between 1 and 4294967295")
		}
		return nil
	}, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
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
	stat := a.fileCmd("stat [workspace] [path]", "Show file or directory attributes", 2, []fileArgument{{label: "Remote path"}}, nil, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		entry, err := service.StatFile(ctx, id, args[0], kibotos.FileOptions{User: user})
		if err != nil {
			return err
		}
		return a.renderFile(entry)
	})

	var byteRange string
	get := a.fileCmd("get [workspace] [path] [local-file|-]", "Download a file (defaults to stdout)", 3, []fileArgument{{label: "Remote path"}}, func(args []string) error {
		if a.printer().IsJSON() && (len(args) < 3 || args[2] == "-" || args[2] == "") {
			return usageErrorWithHint("Pass a local destination file after the remote path.", "files get --json requires a local destination file")
		}
		return validateByteRange(byteRange)
	}, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
		local := "-"
		if len(args) == 2 {
			local = args[1]
		}
		return a.downloadFile(ctx, service, id, args[0], local, kibotos.ReadFileOptions{User: user, Range: byteRange})
	})
	get.Flags().StringVar(&byteRange, "range", "", "Byte range, for example bytes=0-1023")

	var write kibotos.WriteFileOptions
	put := a.fileCmd("put [workspace] [local-file|-] [path]", "Upload a file from disk or stdin", 3,
		[]fileArgument{{label: "Local file path"}, {label: "Remote destination path", defaultValue: uploadDestination}},
		func(_ []string) error { return validateFileMode(write.Mode) }, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
			write.User = user
			return a.uploadFile(ctx, service, id, args[0], args[1], write)
		})
	put.Flags().StringVar(&write.Mode, "mode", "", "Octal permissions, for example 0644")
	put.Flags().BoolVar(&write.CreateOnly, "create-only", false, "Fail if the destination exists")

	var dirMode string
	mkdir := a.fileCmd("mkdir [workspace] [path]", "Create a workspace directory", 2,
		[]fileArgument{{label: "Directory path"}}, func(_ []string) error { return validateFileMode(dirMode) }, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
			entry, err := service.MakeDir(ctx, id, kibotos.MakeDirectoryRequest{Path: args[0], Mode: dirMode, User: user})
			if err != nil {
				return err
			}
			return a.renderFile(entry)
		})
	mkdir.Flags().StringVar(&dirMode, "mode", "", "Octal permissions, for example 0755")
	mv := a.fileCmd("mv [workspace] [source] [destination]", "Move or rename a workspace file", 3,
		[]fileArgument{{label: "Source path"}, {label: "Destination path"}}, nil, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
			entry, err := service.MoveFile(ctx, id, kibotos.MoveFileRequest{Source: args[0], Destination: args[1], User: user})
			if err != nil {
				return err
			}
			return a.renderFile(entry)
		})
	rm := a.fileCmd("rm [workspace] [path]", "Delete a workspace file or directory", 2, []fileArgument{{label: "Remote path to delete"}}, nil, func(ctx context.Context, service *kibotos.WorkspacesService, id string, args []string) error {
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

type fileArgument struct {
	label        string
	defaultValue func([]string) string
}

func (a *App) fileCmd(use, short string, maxArgs int, required []fileArgument,
	validate func([]string) error, run func(context.Context, *kibotos.WorkspacesService, string, []string) error,
) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.MaximumNArgs(maxArgs), RunE: func(cmd *cobra.Command, args []string) error {
		if validate != nil {
			if err := validate(args); err != nil {
				return err
			}
		}
		ref := firstArg(args)
		paths := args[min(len(args), 1):]
		if !a.isInteractive() {
			if strings.TrimSpace(ref) == "" {
				return missingWorkspaceRef()
			}
			if len(paths) < len(required) {
				return usageErrorWithHint("Supply the path arguments shown by `kibo files "+cmd.Name()+" --help`.", "%s is required", required[len(paths)].label)
			}
		}
		for _, path := range paths {
			if path == "" {
				return usageErrorWithHint("Pass a nonempty path.", "path arguments cannot be empty")
			}
		}
		ctx := cmd.Context()
		client, id, err := a.resolveAvailableWorkspace(ctx, ref)
		if err != nil {
			return err
		}
		for _, field := range required[min(len(paths), len(required)):] {
			var defaultValue string
			if field.defaultValue != nil {
				defaultValue = field.defaultValue(paths)
			}
			path, err := a.prompter().Input(field.label, defaultValue, validateNonEmpty)
			if err != nil {
				return err
			}
			paths = append(paths, path)
		}
		return run(ctx, client.Workspaces, id, paths)
	}}
}

func uploadDestination(paths []string) string {
	if paths[0] == "-" {
		return ""
	}
	return "./" + filepath.Base(paths[0])
}

func validateFileMode(mode string) error {
	if mode != "" {
		if _, err := strconv.ParseUint(mode, 8, 12); err != nil {
			return usageErrorWithHint("Pass octal permissions such as 0644 or 0755.", "invalid --mode")
		}
	}
	return nil
}

func validateByteRange(value string) error {
	if value == "" {
		return nil
	}
	unit, ranges, ok := strings.Cut(value, "=")
	if ok && strings.EqualFold(unit, "bytes") {
		valid := true
		for _, part := range strings.Split(ranges, ",") {
			if !validByteRangePart(strings.TrimSpace(part)) {
				valid = false
				break
			}
		}
		if valid {
			return nil
		}
	}
	return usageErrorWithHint("Pass a byte range such as bytes=0-1023.", "invalid --range")
}

func validByteRangePart(part string) bool {
	start, end, ok := strings.Cut(part, "-")
	if !ok || (start == "" && end == "") {
		return false
	}
	first, startErr := strconv.ParseUint(start, 10, 64)
	last, endErr := strconv.ParseUint(end, 10, 64)
	return (start == "" || startErr == nil) && (end == "" || endErr == nil) &&
		(start == "" || end == "" || first <= last)
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

func (a *App) downloadFile(ctx context.Context, service *kibotos.WorkspacesService, id, remote, local string, options kibotos.ReadFileOptions) error {
	p := a.printer()
	body, err := service.ReadFile(ctx, id, remote, options)
	if err != nil {
		return err
	}
	defer body.Close()
	if local == "-" {
		_, err := io.Copy(p.Out, body)
		return err
	}
	written, err := saveDownload(local, body)
	if err != nil {
		return err
	}
	if p.IsJSON() {
		return p.JSON(map[string]any{
			"path": remote, "local_path": local, "size_bytes": written,
			"etag": body.Header.Get("ETag"), "content_range": body.Header.Get("Content-Range"),
		})
	}
	return p.ID(local)
}

func (a *App) uploadFile(ctx context.Context, service *kibotos.WorkspacesService, id, local, remote string, write kibotos.WriteFileOptions) error {
	input := struct{ io.Reader }{a.remainingStdin()}
	if local != "-" {
		file, err := os.Open(local)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Size() > 1<<30 {
			return usageError("local file %q exceeds the 1 GiB upload limit", local)
		}
		input.Reader = file
	}
	result, err := service.WriteFile(ctx, id, remote, input, write)
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

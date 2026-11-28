//go:build !unix

package cli

import (
	"context"
	"os"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

const supportsPTY = false

func (a *App) runPTY(context.Context, *kibotos.Client, string, kibotos.ExecRequest, *os.File) error {
	return usageError("interactive exec requires a Unix terminal")
}

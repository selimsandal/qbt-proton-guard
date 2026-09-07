//go:build darwin

package qbittorrent

import (
	"context"
	"os/exec"
)

func Start(ctx context.Context) error {
	return exec.CommandContext(ctx, "open", "-a", "qBittorrent").Run()
}

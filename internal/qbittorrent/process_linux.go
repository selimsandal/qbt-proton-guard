//go:build linux

package qbittorrent

import (
	"context"
	"os"
	"os/exec"
)

func Start(ctx context.Context) error {
	process := exec.CommandContext(ctx, "qbittorrent")
	process.Stdin = nil
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	if err := process.Start(); err != nil {
		return err
	}
	return process.Process.Release()
}

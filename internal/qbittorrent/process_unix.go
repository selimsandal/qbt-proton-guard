//go:build darwin || linux

package qbittorrent

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

func Running(ctx context.Context) (bool, error) {
	err := exec.CommandContext(ctx, "pgrep", "-x", "qbittorrent").Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func Stop(ctx context.Context) error {
	running, err := Running(ctx)
	if err != nil || !running {
		return err
	}
	if err := exec.CommandContext(ctx, "pkill", "-TERM", "-x", "qbittorrent").Run(); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		running, err := Running(ctx)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errProcessTimeout
}

//go:build windows

package qbittorrent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

func Running(ctx context.Context) (bool, error) {
	output, err := exec.CommandContext(ctx, "tasklist.exe", "/FI", "IMAGENAME eq qbittorrent.exe", "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToLower(string(output)), `"qbittorrent.exe"`), nil
}

func Stop(ctx context.Context) error {
	running, err := Running(ctx)
	if err != nil || !running {
		return err
	}
	_ = exec.CommandContext(ctx, "taskkill.exe", "/IM", "qbittorrent.exe", "/T").Run()
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
	if err := exec.CommandContext(ctx, "taskkill.exe", "/IM", "qbittorrent.exe", "/T", "/F").Run(); err != nil {
		return err
	}
	for index := 0; index < 20; index++ {
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

func Start(ctx context.Context) error {
	paths := []string{
		`C:\Program Files\qBittorrent\qbittorrent.exe`,
		`C:\Program Files (x86)\qBittorrent\qbittorrent.exe`,
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := exec.CommandContext(ctx, "cmd.exe", "/C", "start", "", path).Run(); err == nil {
			return nil
		}
	}
	return errors.New("could not start qBittorrent from a standard installation path")
}

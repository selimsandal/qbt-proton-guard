//go:build !darwin && !linux && !windows

package proton

import (
	"context"
	"fmt"
	"runtime"
)

func detect(context.Context) (Tunnel, error) {
	return Tunnel{}, fmt.Errorf("unsupported operating system %s", runtime.GOOS)
}

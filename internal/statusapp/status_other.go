//go:build !darwin && !linux && !windows

package statusapp

import (
	"context"
	"fmt"
	"runtime"
)

func Run(context.Context) error {
	return fmt.Errorf("status app is unsupported on %s", runtime.GOOS)
}

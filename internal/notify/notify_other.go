//go:build !darwin && !linux && !windows

package notify

import (
	"context"
	"fmt"
	"runtime"
)

func Send(context.Context, string) error {
	return fmt.Errorf("notifications are unsupported on %s", runtime.GOOS)
}

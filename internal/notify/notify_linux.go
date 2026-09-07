//go:build linux

package notify

import (
	"context"
)

func Send(ctx context.Context, message string) error {
	return Queue(ctx, message)
}

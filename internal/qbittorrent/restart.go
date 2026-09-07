package qbittorrent

import (
	"context"
	"fmt"
	"time"
)

func StartAndWait(ctx context.Context) error {
	for attempt := 0; attempt < 2; attempt++ {
		if err := Start(ctx); err != nil {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			running, err := Running(ctx)
			if err != nil {
				return err
			}
			if running {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return fmt.Errorf("qBittorrent did not start")
}

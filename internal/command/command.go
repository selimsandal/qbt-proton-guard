package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func Output(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return "", fmt.Errorf("%s: %w: %s", name, err, text)
	}
	return text, nil
}

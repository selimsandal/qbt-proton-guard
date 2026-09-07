//go:build darwin

package statusapp

import (
	"context"
	"fmt"
)

func Run(context.Context) error {
	return fmt.Errorf("the macOS status app is installed as a native AppKit bundle")
}

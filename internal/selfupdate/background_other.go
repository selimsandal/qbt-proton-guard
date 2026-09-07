//go:build !darwin && !linux && !windows

package selfupdate

import "fmt"

func startBackground(binary, log string) error {
	return fmt.Errorf("background updates are not supported on this platform")
}

//go:build !windows

package guard

import "os"

func replaceStateFile(source, destination string) error {
	return os.Rename(source, destination)
}

//go:build !windows

package qbittorrent

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

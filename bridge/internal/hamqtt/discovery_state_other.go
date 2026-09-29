//go:build !windows

package hamqtt

import (
	"os"
	"path/filepath"
)

func replaceDiscoveryStateFile(from, to string) error {
	return os.Rename(from, to)
}

func syncDiscoveryStateLocation(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

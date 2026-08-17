//go:build !windows

package jinko

import (
	"os"
	"path/filepath"
)

func replaceTokenStateFile(from, to string) error {
	return os.Rename(from, to)
}

func syncTokenStateLocation(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

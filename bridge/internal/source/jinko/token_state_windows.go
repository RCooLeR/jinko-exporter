//go:build windows

package jinko

import "golang.org/x/sys/windows"

func replaceTokenStateFile(from, to string) error {
	fromPtr, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		fromPtr,
		toPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

func syncTokenStateLocation(string) error {
	// MOVEFILE_WRITE_THROUGH asks Windows to flush the replacement before the
	// operation returns; directory handles do not provide a portable Sync.
	return nil
}

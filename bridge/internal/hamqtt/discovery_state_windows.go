//go:build windows

package hamqtt

import "golang.org/x/sys/windows"

func replaceDiscoveryStateFile(from, to string) error {
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

func syncDiscoveryStateLocation(string) error {
	// MOVEFILE_WRITE_THROUGH flushes the replacement before returning. Windows
	// directory handles do not expose a portable os.File.Sync equivalent.
	return nil
}

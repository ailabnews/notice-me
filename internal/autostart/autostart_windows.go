//go:build windows

package autostart

import "golang.org/x/sys/windows/registry"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func setOS(enabled bool, exePath string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		// DeleteValue returns an error if the value doesn't exist; treat that as success.
		if err := k.DeleteValue(appName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringValue(appName, exePath)
}

func isEnabledOS() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, nil
	}
	defer k.Close()
	_, _, err = k.GetStringValue(appName)
	return err == nil, nil
}

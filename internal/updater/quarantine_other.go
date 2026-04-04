//go:build !darwin && !windows

package updater

func clearQuarantine(path string) {}

package updater

import "os"

func clearQuarantine(path string) {
	// Remove the Zone.Identifier alternate data stream (Mark of the Web)
	// that Windows applies to downloaded files. Without this, SmartScreen
	// may block execution.
	os.Remove(path + ":Zone.Identifier")
}

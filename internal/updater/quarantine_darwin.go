package updater

import "os/exec"

func clearQuarantine(path string) {
	exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
	exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
	// Re-sign with ad-hoc signature so AppleSystemPolicy allows execution.
	// Without this, macOS kernel kills downloaded binaries even after xattr removal.
	exec.Command("codesign", "--force", "--sign", "-", path).Run()
}

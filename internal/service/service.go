// Package service manages cross-platform background service registration.
//
// Supported platforms:
//   - macOS: LaunchAgent in ~/Library/LaunchAgents/
//   - Linux: systemd user unit in ~/.config/systemd/user/
//   - Windows: VBScript in the Startup folder
package service

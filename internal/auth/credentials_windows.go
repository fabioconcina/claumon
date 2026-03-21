package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

func loadFromOSCredentialStore() ([]byte, error) {
	// VS Code stores credentials in Windows Credential Manager.
	// Use PowerShell to read it since there's no simple CLI like `security` on macOS.
	script := `
$cred = Get-StoredCredential -Target "Claude Code-credentials" -ErrorAction SilentlyContinue
if ($cred) { [System.Net.NetworkCredential]::new('', $cred.Password).Password }
else {
  # Fall back to cmdkey/CredRead via .NET
  Add-Type -AssemblyName System.Runtime.InteropServices
  $bytes = [System.Text.Encoding]::Unicode.GetBytes((cmdkey /generic:"Claude Code-credentials" /pass 2>$null))
  # Try direct CredentialManager module approach
  $vault = New-Object Windows.Security.Credentials.PasswordVault
  $entry = $vault.Retrieve("Claude Code-credentials", "")
  $entry.RetrievePassword()
  $entry.Password
}
`
	// Simpler approach: use PowerShell CredentialManager or direct registry
	simpleScript := `
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class CredManager {
    [DllImport("advapi32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    public static extern bool CredReadW(string target, int type, int flags, out IntPtr cred);
    [DllImport("advapi32.dll")]
    public static extern void CredFree(IntPtr cred);
    [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)]
    public struct CREDENTIAL {
        public int Flags; public int Type;
        public string TargetName; public string Comment;
        public long LastWritten; public int CredentialBlobSize;
        public IntPtr CredentialBlob; public int Persist;
        public int AttributeCount; public IntPtr Attributes;
        public string TargetAlias; public string UserName;
    }
    public static string Read(string target) {
        IntPtr ptr;
        if (!CredReadW(target, 1, 0, out ptr)) return null;
        var cred = Marshal.PtrToStructure<CREDENTIAL>(ptr);
        var pass = Marshal.PtrToStringUni(cred.CredentialBlob, cred.CredentialBlobSize / 2);
        CredFree(ptr);
        return pass;
    }
}
"@
[CredManager]::Read("Claude Code-credentials")
`

	out, err := exec.Command("powershell", "-NoProfile", "-Command", simpleScript).Output()
	if err != nil {
		return nil, fmt.Errorf("Windows Credential Manager lookup failed: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return nil, fmt.Errorf("no credentials found in Windows Credential Manager")
	}

	return []byte(result), nil
}

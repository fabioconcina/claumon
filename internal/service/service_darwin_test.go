package service

import "testing"

func TestExtractExecPath(t *testing.T) {
	tests := []struct {
		name  string
		plist string
		want  string
	}{
		{
			name: "standard plist",
			plist: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.claumon.dashboard</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/test/.local/bin/claumon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>`,
			want: "/Users/test/.local/bin/claumon",
		},
		{
			name:  "no ProgramArguments",
			plist: `<dict><key>Label</key><string>test</string></dict>`,
			want:  "",
		},
		{
			name:  "empty string",
			plist: "",
			want:  "",
		},
		{
			name: "path with spaces",
			plist: `<key>ProgramArguments</key>
    <array>
        <string>/Users/test/my apps/claumon</string>
    </array>`,
			want: "/Users/test/my apps/claumon",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExecPath(tt.plist)
			if got != tt.want {
				t.Errorf("extractExecPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

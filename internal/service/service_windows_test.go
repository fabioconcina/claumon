package service

import "testing"

func TestExtractVBSPath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "standard script",
			content: "CreateObject(\"WScript.Shell\").Run \"C:\\Users\\test\\claumon.exe\", 0, False\r\n",
			want:    `C:\Users\test\claumon.exe`,
		},
		{
			name:    "path with spaces",
			content: "CreateObject(\"WScript.Shell\").Run \"C:\\Program Files\\claumon\\claumon.exe\", 0, False\r\n",
			want:    `C:\Program Files\claumon\claumon.exe`,
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name:    "no Run keyword",
			content: "something else entirely",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractVBSPath(tt.content)
			if got != tt.want {
				t.Errorf("extractVBSPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

package service

import "testing"

func TestExtractTaskAction(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "standard output",
			output: `HostName:      DESKTOP-ABC
TaskName:      \claumon
Status:        Running
Task To Run:   C:\Users\test\AppData\Local\Programs\claumon\claumon.exe
`,
			want: `C:\Users\test\AppData\Local\Programs\claumon\claumon.exe`,
		},
		{
			name:   "no Task To Run line",
			output: "HostName:      DESKTOP-ABC\nTaskName:      \\claumon\n",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
		{
			name:   "path with spaces",
			output: "Task To Run:   C:\\Program Files\\claumon\\claumon.exe\n",
			want:   `C:\Program Files\claumon\claumon.exe`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTaskAction(tt.output)
			if got != tt.want {
				t.Errorf("extractTaskAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

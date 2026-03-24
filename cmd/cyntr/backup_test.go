package main

import "testing"

func TestIsBackupFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"cyntr.yaml", true},
		{"policy.yaml", true},
		{"sessions.db", true},
		{"cloud-ops-agent.json", true},
		{"hack.sh", false},
		{"malware.exe", false},
		{"readme.md", false},
	}
	for _, tt := range tests {
		got := isBackupFile(tt.name)
		if got != tt.want {
			t.Errorf("isBackupFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

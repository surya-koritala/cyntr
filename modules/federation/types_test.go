package federation

import "testing"

func TestPeerKey(t *testing.T) {
	p := Peer{Name: "us-east-1", Endpoint: "https://cyntr-east.corp.com:8443"}
	if p.Name != "us-east-1" {
		t.Fatalf("expected us-east-1, got %q", p.Name)
	}
}

func TestPeerStatusString(t *testing.T) {
	tests := []struct {
		s    PeerStatus
		want string
	}{
		{PeerHealthy, "healthy"},
		{PeerUnreachable, "unreachable"},
		{PeerSyncing, "syncing"},
		{PeerStatus(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("PeerStatus(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestSyncMessageValidate(t *testing.T) {
	m := SyncMessage{Type: "policy", Version: 1, PeerID: "us-east-1"}
	if m.PeerID != "us-east-1" {
		t.Fatalf("expected peer ID, got %q", m.PeerID)
	}
}

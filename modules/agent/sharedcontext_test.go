package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func newContextStore(t *testing.T) *ContextStore {
	t.Helper()
	cs, err := NewContextStore(filepath.Join(t.TempDir(), "shared_context.db"))
	if err != nil {
		t.Fatalf("NewContextStore: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestContextStoreWriteReadScoped(t *testing.T) {
	cs := newContextStore(t)
	if err := cs.Write(SharedContextEntry{Tenant: "acme", Channel: "batch1", Key: "plan", Content: "step 1, step 2", Author: "architect"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := cs.Read("acme", "batch1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 || got[0].Key != "plan" || got[0].Content != "step 1, step 2" || got[0].Author != "architect" {
		t.Fatalf("read back wrong: %+v", got)
	}
}

func TestContextStoreTenantAndChannelIsolation(t *testing.T) {
	cs := newContextStore(t)
	cs.Write(SharedContextEntry{Tenant: "acme", Channel: "batch1", Key: "plan", Content: "secret", Author: "a"})

	// A different tenant must not see acme's channel.
	if got, _ := cs.Read("globex", "batch1"); len(got) != 0 {
		t.Fatalf("cross-tenant leak: %+v", got)
	}
	// A different channel in the same tenant must not see it either.
	if got, _ := cs.Read("acme", "batch2"); len(got) != 0 {
		t.Fatalf("cross-channel leak: %+v", got)
	}
	// Empty scope reads nothing.
	if got, _ := cs.Read("", "batch1"); len(got) != 0 {
		t.Fatalf("empty tenant should read nothing: %+v", got)
	}
}

func TestContextStoreWriteIsUpsert(t *testing.T) {
	cs := newContextStore(t)
	cs.Write(SharedContextEntry{Tenant: "acme", Channel: "b", Key: "plan", Content: "v1", Author: "a"})
	cs.Write(SharedContextEntry{Tenant: "acme", Channel: "b", Key: "plan", Content: "v2", Author: "a"})
	got, _ := cs.Read("acme", "b")
	if len(got) != 1 || got[0].Content != "v2" {
		t.Fatalf("re-write should overwrite same key in place: %+v", got)
	}
}

func TestContextStoreWriteRejectsIncompleteScope(t *testing.T) {
	cs := newContextStore(t)
	for _, e := range []SharedContextEntry{
		{Channel: "b", Key: "k", Content: "c"},       // no tenant
		{Tenant: "acme", Key: "k", Content: "c"},     // no channel
		{Tenant: "acme", Channel: "b", Content: "c"}, // no key
	} {
		if err := cs.Write(e); err == nil {
			t.Fatalf("expected error for incomplete scope %+v", e)
		}
	}
}

func TestContextStoreClear(t *testing.T) {
	cs := newContextStore(t)
	cs.Write(SharedContextEntry{Tenant: "acme", Channel: "b", Key: "k", Content: "c", Author: "a"})
	if err := cs.Clear("acme", "b"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got, _ := cs.Read("acme", "b"); len(got) != 0 {
		t.Fatalf("Clear left rows: %+v", got)
	}
}

func TestFormatSharedContext(t *testing.T) {
	if out := FormatSharedContext(nil); out == "" {
		t.Fatal("empty format should still return guidance text")
	}
	out := FormatSharedContext([]SharedContextEntry{{Key: "plan", Content: "do X", Author: "architect"}})
	if !strings.Contains(out, "plan") || !strings.Contains(out, "do X") || !strings.Contains(out, "architect") {
		t.Fatalf("format missing fields: %q", out)
	}
}

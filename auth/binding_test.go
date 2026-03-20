package auth

import "testing"

func TestIdentityBindingBindAndResolve(t *testing.T) {
	ib := NewIdentityBinding()
	ib.Bind("slack", "U123", "jane@corp.com")

	id, ok := ib.Resolve("slack", "U123")
	if !ok {
		t.Fatal("expected found")
	}
	if id != "jane@corp.com" {
		t.Fatalf("got %q", id)
	}
}

func TestIdentityBindingResolveNotFound(t *testing.T) {
	ib := NewIdentityBinding()
	_, ok := ib.Resolve("slack", "unknown")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestIdentityBindingCrossChannel(t *testing.T) {
	ib := NewIdentityBinding()
	ib.Bind("slack", "U123", "jane@corp.com")
	ib.Bind("telegram", "67890", "jane@corp.com")
	ib.Bind("discord", "D456", "jane@corp.com")

	// All should resolve to same principal
	for _, tc := range []struct{ ch, uid string }{
		{"slack", "U123"}, {"telegram", "67890"}, {"discord", "D456"},
	} {
		id, ok := ib.Resolve(tc.ch, tc.uid)
		if !ok || id != "jane@corp.com" {
			t.Fatalf("%s/%s: got %q", tc.ch, tc.uid, id)
		}
	}
}

func TestIdentityBindingGetBindings(t *testing.T) {
	ib := NewIdentityBinding()
	ib.Bind("slack", "U1", "jane@corp.com")
	ib.Bind("telegram", "T1", "jane@corp.com")

	bindings := ib.GetBindings("jane@corp.com")
	if len(bindings) != 2 {
		t.Fatalf("expected 2, got %d", len(bindings))
	}
}

func TestIdentityBindingUnbind(t *testing.T) {
	ib := NewIdentityBinding()
	ib.Bind("slack", "U1", "jane@corp.com")
	ib.Unbind("slack", "U1")

	_, ok := ib.Resolve("slack", "U1")
	if ok {
		t.Fatal("expected unbound")
	}
}

func TestIdentityBindingCount(t *testing.T) {
	ib := NewIdentityBinding()
	ib.Bind("slack", "U1", "jane@corp.com")
	ib.Bind("telegram", "T1", "bob@corp.com")
	if ib.Count() != 2 {
		t.Fatalf("expected 2, got %d", ib.Count())
	}
}

func TestIdentityBindingIsolation(t *testing.T) {
	ib := NewIdentityBinding()
	ib.Bind("slack", "U1", "jane@corp.com")
	ib.Bind("slack", "U2", "bob@corp.com")

	id, _ := ib.Resolve("slack", "U1")
	if id != "jane@corp.com" {
		t.Fatalf("got %q", id)
	}
	id, _ = ib.Resolve("slack", "U2")
	if id != "bob@corp.com" {
		t.Fatalf("got %q", id)
	}
}

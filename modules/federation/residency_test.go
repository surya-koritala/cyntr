package federation

import "testing"

func TestResidencyPolicyAllowsUnrestricted(t *testing.T) {
	rp := NewResidencyPolicy()
	if err := rp.Check("marketing", "us-east"); err != nil {
		t.Fatalf("expected allowed: %v", err)
	}
}

func TestResidencyPolicyEnforces(t *testing.T) {
	rp := NewResidencyPolicy()
	rp.SetRule("finance-eu", "eu-west-1")

	// Correct node
	if err := rp.Check("finance-eu", "eu-west-1"); err != nil {
		t.Fatalf("should be allowed: %v", err)
	}

	// Wrong node
	if err := rp.Check("finance-eu", "us-east-1"); err == nil {
		t.Fatal("expected residency violation")
	}
}

func TestResidencyPolicyRemove(t *testing.T) {
	rp := NewResidencyPolicy()
	rp.SetRule("finance", "eu-west-1")
	rp.RemoveRule("finance")

	if err := rp.Check("finance", "us-east-1"); err != nil {
		t.Fatalf("should be allowed after remove: %v", err)
	}
}

func TestResidencyPolicyGetRule(t *testing.T) {
	rp := NewResidencyPolicy()
	rp.SetRule("finance", "eu-west-1")

	node, ok := rp.GetRule("finance")
	if !ok {
		t.Fatal("expected rule")
	}
	if node != "eu-west-1" {
		t.Fatalf("got %q", node)
	}

	_, ok = rp.GetRule("marketing")
	if ok {
		t.Fatal("expected no rule")
	}
}

func TestResidencyPolicyListRules(t *testing.T) {
	rp := NewResidencyPolicy()
	rp.SetRule("finance", "eu-west-1")
	rp.SetRule("healthcare", "us-hipaa-1")

	rules := rp.ListRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2, got %d", len(rules))
	}
	if rules["finance"] != "eu-west-1" {
		t.Fatalf("got %q", rules["finance"])
	}
}

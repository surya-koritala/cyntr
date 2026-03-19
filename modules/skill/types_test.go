package skill

import "testing"

func TestSkillManifestValidate(t *testing.T) {
	m := SkillManifest{
		Name:    "test-skill",
		Version: "1.0.0",
		Author:  "test",
		License: "Apache-2.0",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestSkillManifestValidateMissingName(t *testing.T) {
	m := SkillManifest{Version: "1.0.0"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestSkillManifestValidateMissingVersion(t *testing.T) {
	m := SkillManifest{Name: "test"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestCapabilitiesHasNetwork(t *testing.T) {
	c := Capabilities{Network: []string{"https://*.example.com"}}
	if !c.HasNetwork() {
		t.Fatal("expected has network")
	}
}

func TestCapabilitiesNoNetwork(t *testing.T) {
	c := Capabilities{}
	if c.HasNetwork() {
		t.Fatal("expected no network")
	}
}

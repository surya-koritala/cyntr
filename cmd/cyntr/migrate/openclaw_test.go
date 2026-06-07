package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const fixtureSkillMD = `---
name: weather-checker
description: Check the weather for a city
version: 1.2.0
author: community-dev
tools:
  - name: http_request
---

# Weather Checker

Use http_request to fetch weather data.
`

const fixtureSkillMD2 = `---
name: calc
description: do math
version: 0.1.0
author: someone
---

# Calc

Add numbers.
`

// writeFixtureOpenClaw builds a temp ~/.openclaw with skills + config and
// returns its path.
func writeFixtureOpenClaw(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mkSkill := func(name, content string) {
		sd := filepath.Join(dir, "skills", name)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkSkill("weather", fixtureSkillMD)
	mkSkill("calc", fixtureSkillMD2)

	cfg := "listen:\n  address: \"127.0.0.1:9000\"\nauth:\n  provider: \"oidc\"\n  issuer: \"https://issuer.example\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newOpts(t *testing.T, ocDir string) (Options, string, string) {
	t.Helper()
	work := t.TempDir()
	skillsDir := filepath.Join(work, "skills")
	cfgPath := filepath.Join(work, "cyntr.yaml")
	return Options{
		OpenClawDir: ocDir,
		ConfigPath:  cfgPath,
		SkillsDir:   skillsDir,
		Out:         &bytes.Buffer{},
	}, skillsDir, cfgPath
}

func TestMigrateImportsSkillsViaCompat(t *testing.T) {
	oc := writeFixtureOpenClaw(t)
	opts, skillsDir, _ := newOpts(t, oc)

	rep, err := RunMigrateOpenClaw(opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// compat prefixes names with "openclaw-".
	want := map[string]string{
		"openclaw-weather-checker": "1.2.0",
		"openclaw-calc":            "0.1.0",
	}
	got := map[string]string{}
	for _, s := range rep.Skills {
		if s.Err != "" {
			t.Fatalf("unexpected skill error for %s: %s", s.Source, s.Err)
		}
		got[s.Name] = s.Version
	}
	for n, v := range want {
		if got[n] != v {
			t.Fatalf("skill %s: want version %s, got %q (all=%v)", n, v, got[n], got)
		}
	}

	// Each imported skill must be on disk as a canonical Cyntr skill dir.
	for name := range want {
		manifest := filepath.Join(skillsDir, name, "skill.yaml")
		if _, err := os.Stat(manifest); err != nil {
			t.Fatalf("expected skill.yaml for %s: %v", name, err)
		}
		instr := filepath.Join(skillsDir, name, "skill.md")
		if _, err := os.Stat(instr); err != nil {
			t.Fatalf("expected skill.md for %s: %v", name, err)
		}
		// Manifest must carry the restricted (no-shell) capabilities the
		// compat loader produced.
		data, _ := os.ReadFile(manifest)
		if strings.Contains(string(data), "shell: true") {
			t.Fatalf("imported skill %s should not have shell access", name)
		}
	}
}

func TestMigrateImportsConfig(t *testing.T) {
	oc := writeFixtureOpenClaw(t)
	opts, _, cfgPath := newOpts(t, oc)

	if _, err := RunMigrateOpenClaw(opts); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	var cfg struct {
		Listen struct {
			Address string `yaml:"address"`
		} `yaml:"listen"`
		Auth struct {
			Provider string `yaml:"provider"`
			Issuer   string `yaml:"issuer"`
		} `yaml:"auth"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.Listen.Address != "127.0.0.1:9000" {
		t.Fatalf("listen.address not imported, got %q", cfg.Listen.Address)
	}
	if cfg.Auth.Provider != "oidc" || cfg.Auth.Issuer != "https://issuer.example" {
		t.Fatalf("auth not imported: %+v", cfg.Auth)
	}
}

func TestMigrateDryRunMutatesNothing(t *testing.T) {
	oc := writeFixtureOpenClaw(t)
	opts, skillsDir, cfgPath := newOpts(t, oc)
	opts.DryRun = true

	rep, err := RunMigrateOpenClaw(opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Plan still computed.
	if len(rep.Skills) != 2 {
		t.Fatalf("expected 2 skills planned, got %d", len(rep.Skills))
	}
	if len(rep.Applied) != 0 {
		t.Fatalf("dry-run must apply nothing, applied=%v", rep.Applied)
	}

	// Nothing written: no config file, no skills dir entries, no record.
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote config file: err=%v", err)
	}
	if entries, _ := os.ReadDir(skillsDir); len(entries) != 0 {
		t.Fatalf("dry-run wrote into skills dir: %v", entries)
	}
}

func TestMigrateConflictsReportedNotOverwritten(t *testing.T) {
	oc := writeFixtureOpenClaw(t)
	opts, skillsDir, cfgPath := newOpts(t, oc)

	// Pre-existing Cyntr skill with the same (prefixed) name as one import.
	existingDir := filepath.Join(skillsDir, "openclaw-weather-checker")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(existingDir, "skill.md")
	if err := os.WriteFile(sentinel, []byte("ORIGINAL CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-existing Cyntr config with auth.provider already set.
	preCfg := "listen:\n  address: \"\"\nauth:\n  provider: \"entra\"\n"
	if err := os.WriteFile(cfgPath, []byte(preCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := RunMigrateOpenClaw(opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// weather-checker must be reported as a conflict; calc as an import.
	var weatherConflict, calcImported bool
	for _, s := range rep.Skills {
		if s.Name == "openclaw-weather-checker" && s.Action == actionConflict {
			weatherConflict = true
		}
		if s.Name == "openclaw-calc" && s.Action == actionCreate {
			calcImported = true
		}
	}
	if !weatherConflict {
		t.Fatalf("expected weather-checker conflict, skills=%+v", rep.Skills)
	}
	if !calcImported {
		t.Fatalf("expected calc imported, skills=%+v", rep.Skills)
	}

	// The conflicting skill must NOT be overwritten.
	data, _ := os.ReadFile(sentinel)
	if string(data) != "ORIGINAL CONTENT" {
		t.Fatalf("conflicting skill was overwritten: %q", string(data))
	}

	// auth.provider conflict must be reported and the original preserved.
	var providerConflict bool
	for _, c := range rep.Config {
		if c.Field == "auth.provider" && c.Action == actionConflict {
			providerConflict = true
		}
	}
	if !providerConflict {
		t.Fatalf("expected auth.provider conflict, config=%+v", rep.Config)
	}
	written, _ := os.ReadFile(cfgPath)
	var cfg struct {
		Auth struct {
			Provider string `yaml:"provider"`
			Issuer   string `yaml:"issuer"`
		} `yaml:"auth"`
	}
	yaml.Unmarshal(written, &cfg)
	if cfg.Auth.Provider != "entra" {
		t.Fatalf("auth.provider was overwritten, got %q", cfg.Auth.Provider)
	}
	// But a non-conflicting field (auth.issuer was empty) should be imported.
	if cfg.Auth.Issuer != "https://issuer.example" {
		t.Fatalf("non-conflicting auth.issuer should import, got %q", cfg.Auth.Issuer)
	}
}

func TestMigrateOpenClawDirEnvOverride(t *testing.T) {
	oc := writeFixtureOpenClaw(t)
	t.Setenv("CYNTR_OPENCLAW_DIR", oc)

	work := t.TempDir()
	opts := Options{
		ConfigPath: filepath.Join(work, "cyntr.yaml"),
		SkillsDir:  filepath.Join(work, "skills"),
		DryRun:     true,
		Out:        &bytes.Buffer{},
	}
	rep, err := RunMigrateOpenClaw(opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.OpenClawDir != oc {
		t.Fatalf("env override not used: got %q want %q", rep.OpenClawDir, oc)
	}
	if len(rep.Skills) != 2 {
		t.Fatalf("expected 2 skills via env dir, got %d", len(rep.Skills))
	}
}

func TestMigrateMissingHomeIsError(t *testing.T) {
	opts := Options{
		OpenClawDir: filepath.Join(t.TempDir(), "does-not-exist"),
		Out:         &bytes.Buffer{},
	}
	if _, err := RunMigrateOpenClaw(opts); err == nil {
		t.Fatal("expected error for missing openclaw home")
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Options
	}{
		{"dry-run", []string{"--dry-run"}, Options{DryRun: true}},
		{"tenant", []string{"--tenant", "acme"}, Options{Tenant: "acme"}},
		{"dir+config", []string{"--dir", "/x", "--config", "/y.yaml"}, Options{OpenClawDir: "/x", ConfigPath: "/y.yaml"}},
		{"skills-dir", []string{"--skills-dir", "/s"}, Options{SkillsDir: "/s"}},
		{"eq-form", []string{"--dry-run=true"}, Options{DryRun: true}},
		{"none", nil, Options{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseArgs(tt.args)
			if got != tt.want {
				t.Fatalf("ParseArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestMigrateProducesRecord(t *testing.T) {
	oc := writeFixtureOpenClaw(t)
	opts, skillsDir, _ := newOpts(t, oc)
	if _, err := RunMigrateOpenClaw(opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	entries, _ := os.ReadDir(skillsDir)
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "openclaw-migration-") && strings.HasSuffix(e.Name(), ".json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a migration record json in %s, got %v", skillsDir, entries)
	}
}

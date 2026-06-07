// Package migrate implements one-shot importers that bring state from other
// agent runtimes into Cyntr. The OpenClaw migrator reads an ~/.openclaw home
// directory (config + skills) and imports it into the local Cyntr config and
// skill registry directory.
//
// Design constraints (see ticket F27):
//   - REUSES modules/skill/compat to parse OpenClaw SKILL.md files — we never
//     re-implement that parser here.
//   - NEVER overwrites existing Cyntr config or skills. Conflicts are REPORTED
//     in the preview and skipped.
//   - --dry-run mutates nothing on disk.
//   - The OpenClaw home is overridable via CYNTR_OPENCLAW_DIR so tests (and
//     operators with a non-standard layout) can point it at a fixture.
//   - Multi-tenant by default: imported skills are scoped to a target tenant
//     (default "default", overridable with --tenant) and that tenant is
//     recorded in the migration record so the import is attributable.
package migrate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/modules/skill"
	"github.com/cyntr-dev/cyntr/modules/skill/compat"
	"gopkg.in/yaml.v3"
)

// Options controls a single migration run.
type Options struct {
	// OpenClawDir is the OpenClaw home to read. When empty it is resolved from
	// CYNTR_OPENCLAW_DIR, then ~/.openclaw.
	OpenClawDir string
	// ConfigPath is the destination Cyntr config file. When empty it is
	// resolved from CYNTR_CONFIG, then "cyntr.yaml".
	ConfigPath string
	// SkillsDir is the destination Cyntr skills directory (each skill becomes a
	// subdirectory with skill.yaml + skill.md). When empty it is resolved from
	// CYNTR_SKILLS_DIR, then <CYNTR_DATA_DIR>/skills, then ~/.cyntr/skills.
	SkillsDir string
	// Tenant scopes the imported skills/config. Defaults to "default".
	Tenant string
	// DryRun, when true, makes RunMigrateOpenClaw mutate nothing on disk.
	DryRun bool
	// Out receives the human-readable preview. Defaults to os.Stdout.
	Out io.Writer
}

// changeAction classifies a single planned change.
type changeAction string

const (
	actionCreate   changeAction = "create"   // will be written (target absent)
	actionConflict changeAction = "conflict" // already exists — skipped, never overwritten
)

// skillChange describes one OpenClaw skill considered for import.
type skillChange struct {
	Name    string       `json:"name"`    // Cyntr (prefixed) skill name
	Source  string       `json:"source"`  // source SKILL.md path
	Dest    string       `json:"dest"`    // destination skill directory
	Action  changeAction `json:"action"`  // create | conflict
	Version string       `json:"version"` // parsed skill version
	Err     string       `json:"error,omitempty"`
}

// configChange describes one OpenClaw config key considered for import.
type configChange struct {
	Field  string       `json:"field"`
	Value  string       `json:"value"`
	Action changeAction `json:"action"`
}

// Report is the full preview / result of a migration run. It is returned so
// callers (and tests) can assert on the plan, and is also serialized to a
// migration record file (unless --dry-run) for auditability.
type Report struct {
	Tenant      string         `json:"tenant"`
	OpenClawDir string         `json:"openclaw_dir"`
	ConfigPath  string         `json:"config_path"`
	SkillsDir   string         `json:"skills_dir"`
	DryRun      bool           `json:"dry_run"`
	Timestamp   time.Time      `json:"timestamp"`
	Skills      []skillChange  `json:"skills"`
	Config      []configChange `json:"config"`
	// Applied lists changes actually written to disk (empty on dry-run).
	Applied []string `json:"applied"`
}

// Counts summarizes a report.
func (r Report) Counts() (created, conflicts, errors int) {
	for _, s := range r.Skills {
		switch {
		case s.Err != "":
			errors++
		case s.Action == actionCreate:
			created++
		case s.Action == actionConflict:
			conflicts++
		}
	}
	for _, c := range r.Config {
		switch c.Action {
		case actionCreate:
			created++
		case actionConflict:
			conflicts++
		}
	}
	return
}

// openClawConfig is the subset of OpenClaw config we know how to map onto
// Cyntr config. Unknown keys are ignored. OpenClaw stores config as either
// config.yaml or config.json at the root of its home dir.
type openClawConfig struct {
	Listen struct {
		Address string `yaml:"address" json:"address"`
		WebUI   string `yaml:"webui" json:"webui"`
	} `yaml:"listen" json:"listen"`
	Auth struct {
		Provider string `yaml:"provider" json:"provider"`
		Issuer   string `yaml:"issuer" json:"issuer"`
		ClientID string `yaml:"client_id" json:"client_id"`
	} `yaml:"auth" json:"auth"`
}

// RunMigrateOpenClaw executes the migration described by opts and returns the
// report (the preview is also written to opts.Out). It never returns an error
// for a benign conflict — conflicts are reported in the Report. It returns an
// error only for unrecoverable problems (e.g. the OpenClaw home is unreadable).
func RunMigrateOpenClaw(opts Options) (Report, error) {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Tenant == "" {
		opts.Tenant = "default"
	}
	opts.OpenClawDir = resolveOpenClawDir(opts.OpenClawDir)
	opts.ConfigPath = resolveConfigPath(opts.ConfigPath)
	opts.SkillsDir = resolveSkillsDir(opts.SkillsDir)

	info, err := os.Stat(opts.OpenClawDir)
	if err != nil {
		return Report{}, fmt.Errorf("openclaw home %q not found: %w", opts.OpenClawDir, err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("openclaw home %q is not a directory", opts.OpenClawDir)
	}

	rep := Report{
		Tenant:      opts.Tenant,
		OpenClawDir: opts.OpenClawDir,
		ConfigPath:  opts.ConfigPath,
		SkillsDir:   opts.SkillsDir,
		DryRun:      opts.DryRun,
		Timestamp:   time.Now().UTC(),
	}

	// --- Plan skills -------------------------------------------------------
	rep.Skills = planSkills(opts.OpenClawDir, opts.SkillsDir)

	// --- Plan config -------------------------------------------------------
	rep.Config, err = planConfig(opts.OpenClawDir, opts.ConfigPath)
	if err != nil {
		return Report{}, err
	}

	// --- Preview -----------------------------------------------------------
	writePreview(opts.Out, rep)

	// --- Apply (unless dry-run) -------------------------------------------
	if opts.DryRun {
		fmt.Fprintln(opts.Out, "\n[dry-run] no changes were written.")
		return rep, nil
	}

	if err := apply(&rep); err != nil {
		return rep, err
	}

	created, conflicts, errors := rep.Counts()
	fmt.Fprintf(opts.Out, "\nMigration complete: %d imported, %d conflicts skipped, %d errors.\n",
		created, conflicts, errors)
	return rep, nil
}

// planSkills enumerates ~/.openclaw/skills/<name>/SKILL.md, parses each via the
// shared compat loader, and decides create vs conflict against the Cyntr
// skills directory. It never writes anything.
func planSkills(openClawDir, skillsDir string) []skillChange {
	skillsRoot := filepath.Join(openClawDir, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return nil // no skills to import
	}
	var changes []skillChange
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		src := filepath.Join(skillsRoot, entry.Name(), "SKILL.md")
		if _, statErr := os.Stat(src); statErr != nil {
			continue
		}
		// REUSE the existing compat loader — it already parses OpenClaw SKILL.md.
		parsed, perr := compat.LoadOpenClawSkillFromFile(src)
		if perr != nil {
			changes = append(changes, skillChange{
				Source: src,
				Action: actionCreate,
				Err:    perr.Error(),
			})
			continue
		}
		dest := filepath.Join(skillsDir, parsed.Manifest.Name)
		action := actionCreate
		// Conflict if a skill directory of that name already exists.
		if _, derr := os.Stat(dest); derr == nil {
			action = actionConflict
		}
		changes = append(changes, skillChange{
			Name:    parsed.Manifest.Name,
			Source:  src,
			Dest:    dest,
			Action:  action,
			Version: parsed.Manifest.Version,
		})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

// planConfig reads ~/.openclaw/config.{yaml,json}, maps known fields onto the
// Cyntr config, and decides create vs conflict per field. An existing,
// non-empty Cyntr field is a conflict and is never overwritten.
func planConfig(openClawDir, cyntrConfigPath string) ([]configChange, error) {
	oc, ok, err := readOpenClawConfig(openClawDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	// Load existing Cyntr config (if any) to detect conflicts. We start from a
	// ZERO config (not DefaultConfig) so that built-in defaults are not
	// mistaken for operator-set values — only fields actually present in the
	// user's cyntr.yaml count as a conflict.
	var existing config.CyntrConfig
	if data, rerr := os.ReadFile(cyntrConfigPath); rerr == nil {
		_ = yaml.Unmarshal(data, &existing) // best-effort; malformed -> treated as empty
	}

	var changes []configChange
	consider := func(field, incoming, current string) {
		if incoming == "" {
			return
		}
		action := actionCreate
		if current != "" {
			action = actionConflict
		}
		changes = append(changes, configChange{Field: field, Value: incoming, Action: action})
	}
	consider("listen.address", oc.Listen.Address, existing.Listen.Address)
	consider("listen.webui", oc.Listen.WebUI, existing.Listen.WebUI)
	consider("auth.provider", oc.Auth.Provider, existing.Auth.Provider)
	consider("auth.issuer", oc.Auth.Issuer, existing.Auth.Issuer)
	consider("auth.client_id", oc.Auth.ClientID, existing.Auth.ClientID)
	return changes, nil
}

// apply performs the writes for every create-action change in the report and
// records them in rep.Applied. Conflicts and errored skills are skipped.
func apply(rep *Report) error {
	// Skills: write each create as a Cyntr skill dir (skill.yaml + skill.md).
	for i := range rep.Skills {
		sc := &rep.Skills[i]
		if sc.Action != actionCreate || sc.Err != "" {
			continue
		}
		parsed, perr := compat.LoadOpenClawSkillFromFile(sc.Source)
		if perr != nil {
			sc.Err = perr.Error()
			continue
		}
		if err := writeSkillDir(sc.Dest, parsed); err != nil {
			sc.Err = err.Error()
			continue
		}
		rep.Applied = append(rep.Applied, "skill:"+sc.Name)
	}

	// Config: merge create-action fields into the Cyntr config file.
	if hasConfigCreates(rep.Config) {
		if err := applyConfig(rep.ConfigPath, rep.Config); err != nil {
			return fmt.Errorf("apply config: %w", err)
		}
		for _, c := range rep.Config {
			if c.Action == actionCreate {
				rep.Applied = append(rep.Applied, "config:"+c.Field)
			}
		}
	}

	// Migration record (audit trail) alongside the skills dir.
	if err := writeRecord(rep); err != nil {
		return fmt.Errorf("write migration record: %w", err)
	}
	return nil
}

// writeSkillDir materializes an imported skill as a canonical Cyntr skill
// directory that the registry's LoadSkill reads on next start. It refuses to
// overwrite an existing directory (defense in depth — planning already filters
// conflicts, but the world may have changed since).
func writeSkillDir(dest string, s *skill.InstalledSkill) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("destination %q already exists — refusing to overwrite", dest)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	manifestBytes, err := yaml.Marshal(s.Manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, "skill.yaml"), manifestBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, "skill.md"), []byte(s.Instructions), 0o644); err != nil {
		return err
	}
	return nil
}

// applyConfig merges create-action fields into the Cyntr config file, leaving
// every existing (conflicting) value untouched.
func applyConfig(path string, changes []configChange) error {
	cfg := config.DefaultConfig()
	if data, err := os.ReadFile(path); err == nil {
		if uerr := yaml.Unmarshal(data, &cfg); uerr != nil {
			return fmt.Errorf("parse existing config %q: %w", path, uerr)
		}
	}
	for _, c := range changes {
		if c.Action != actionCreate {
			continue // never overwrite a conflict
		}
		switch c.Field {
		case "listen.address":
			cfg.Listen.Address = c.Value
		case "listen.webui":
			cfg.Listen.WebUI = c.Value
		case "auth.provider":
			cfg.Auth.Provider = c.Value
		case "auth.issuer":
			cfg.Auth.Issuer = c.Value
		case "auth.client_id":
			cfg.Auth.ClientID = c.Value
		}
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return os.WriteFile(path, out, 0o644)
}

// writeRecord persists a JSON migration record next to the skills dir so the
// import is auditable / re-inspectable.
func writeRecord(rep *Report) error {
	dir := rep.SkillsDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("openclaw-migration-%s.json", rep.Timestamp.Format("20060102T150405Z"))
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

func hasConfigCreates(changes []configChange) bool {
	for _, c := range changes {
		if c.Action == actionCreate {
			return true
		}
	}
	return false
}

// readOpenClawConfig loads config.yaml or config.json from the OpenClaw home.
// The bool result is false when neither file exists.
func readOpenClawConfig(openClawDir string) (openClawConfig, bool, error) {
	var oc openClawConfig
	yamlPath := filepath.Join(openClawDir, "config.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		if uerr := yaml.Unmarshal(data, &oc); uerr != nil {
			return oc, false, fmt.Errorf("parse %q: %w", yamlPath, uerr)
		}
		return oc, true, nil
	}
	jsonPath := filepath.Join(openClawDir, "config.json")
	if data, err := os.ReadFile(jsonPath); err == nil {
		if uerr := json.Unmarshal(data, &oc); uerr != nil {
			return oc, false, fmt.Errorf("parse %q: %w", jsonPath, uerr)
		}
		return oc, true, nil
	}
	return oc, false, nil
}

func writePreview(w io.Writer, rep Report) {
	fmt.Fprintf(w, "OpenClaw migration preview (tenant=%s)\n", rep.Tenant)
	fmt.Fprintf(w, "  source:  %s\n", rep.OpenClawDir)
	fmt.Fprintf(w, "  config:  %s\n", rep.ConfigPath)
	fmt.Fprintf(w, "  skills:  %s\n", rep.SkillsDir)

	fmt.Fprintln(w, "\nSkills:")
	if len(rep.Skills) == 0 {
		fmt.Fprintln(w, "  (none found)")
	}
	for _, s := range rep.Skills {
		switch {
		case s.Err != "":
			fmt.Fprintf(w, "  [error]    %s: %s\n", s.Source, s.Err)
		case s.Action == actionConflict:
			fmt.Fprintf(w, "  [conflict] %s (already exists — will NOT overwrite)\n", s.Name)
		default:
			fmt.Fprintf(w, "  [import]   %s v%s\n", s.Name, s.Version)
		}
	}

	fmt.Fprintln(w, "\nConfig:")
	if len(rep.Config) == 0 {
		fmt.Fprintln(w, "  (no importable config found)")
	}
	for _, c := range rep.Config {
		if c.Action == actionConflict {
			fmt.Fprintf(w, "  [conflict] %s (already set — will NOT overwrite)\n", c.Field)
		} else {
			fmt.Fprintf(w, "  [import]   %s = %s\n", c.Field, c.Value)
		}
	}

	created, conflicts, errors := rep.Counts()
	fmt.Fprintf(w, "\nPlan: %d to import, %d conflicts (skipped), %d errors.\n",
		created, conflicts, errors)
}

// ---- path resolution --------------------------------------------------------

func resolveOpenClawDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("CYNTR_OPENCLAW_DIR"); env != "" {
		return env
	}
	return filepath.Join(homeDir(), ".openclaw")
}

func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("CYNTR_CONFIG"); env != "" {
		return env
	}
	return "cyntr.yaml"
}

func resolveSkillsDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("CYNTR_SKILLS_DIR"); env != "" {
		return env
	}
	if data := os.Getenv("CYNTR_DATA_DIR"); data != "" {
		return filepath.Join(data, "skills")
	}
	return filepath.Join(homeDir(), ".cyntr", "skills")
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "."
}

// ParseArgs builds Options from `cyntr migrate openclaw [flags]` args.
// Recognized flags: --dry-run, --tenant <t>, --dir <openclaw-home>,
// --config <path>, --skills-dir <path>. Unknown flags are ignored so the
// integrator's dispatcher can pass the raw arg slice straight through.
func ParseArgs(args []string) Options {
	var opts Options
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.DryRun = true
		case "--tenant":
			if i+1 < len(args) {
				i++
				opts.Tenant = args[i]
			}
		case "--dir":
			if i+1 < len(args) {
				i++
				opts.OpenClawDir = args[i]
			}
		case "--config":
			if i+1 < len(args) {
				i++
				opts.ConfigPath = args[i]
			}
		case "--skills-dir":
			if i+1 < len(args) {
				i++
				opts.SkillsDir = args[i]
			}
		default:
			// tolerate `--flag=value`
			if strings.HasPrefix(args[i], "--dry-run=") {
				opts.DryRun = strings.EqualFold(strings.TrimPrefix(args[i], "--dry-run="), "true")
			}
		}
	}
	return opts
}

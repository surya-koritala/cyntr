package agent

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	_ "modernc.org/sqlite"
)

var personalityLogger = log.Default().WithModule("agent_personality")

// ErrUnknownPersonality is returned when a persona name is selected (or fetched)
// that does not exist in the caller's tenant catalog. Callers compare with
// errors.Is so the CLI / chat handler can surface a clean "unknown personality"
// message instead of leaking a raw store error.
var ErrUnknownPersonality = errors.New("unknown personality")

// Personality is a named system-prompt fragment selectable per conversation via
// '/personality <name>'. The fragment is prepended to the assembled system
// context (it does NOT replace the per-workspace context-file loader or the
// agent's base system prompt — it composes with them).
type Personality struct {
	Tenant    string    `json:"tenant"`
	Name      string    `json:"name"`
	Prompt    string    `json:"prompt"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultPersonalities are seeded into every tenant the first time its catalog
// is opened. They are upserted only when absent, so a tenant that has edited a
// default keeps its edit across restarts.
var DefaultPersonalities = []Personality{
	{
		Name:   "default",
		Prompt: "You are a helpful, balanced assistant. Answer clearly and completely.",
	},
	{
		Name:   "concise",
		Prompt: "Be extremely concise. Prefer short, direct answers. Omit preamble, caveats, and filler. Use lists only when they shorten the answer.",
	},
	{
		Name:   "friendly",
		Prompt: "Adopt a warm, friendly, encouraging tone. Be approachable and supportive while staying accurate and helpful.",
	},
}

// PersonalityStore persists per-tenant named system-prompt fragments to SQLite
// and tracks, per session, which persona is currently active. The on-disk
// catalog mirrors the other agent stores (memory/shared-context): every row is
// tenant-scoped and no read ever crosses a tenant boundary. The active-selection
// state is in-memory (per session, per tenant) — selection is a property of a
// live conversation, not durable catalog data — and survives for the life of the
// process so it persists across turns within a session.
type PersonalityStore struct {
	mu sync.Mutex
	db *sql.DB

	// active maps "tenant\x00session" -> persona name. Keyed with a NUL
	// separator so neither component can spoof another's selection.
	selMu  sync.RWMutex
	active map[string]string

	// seeded tracks which tenants have had their defaults seeded this process,
	// so the seed query runs at most once per tenant.
	seeded map[string]bool
}

// NewPersonalityStore opens or creates the personalities database.
func NewPersonalityStore(dbPath string) (*PersonalityStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open personality db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS personalities (
			tenant TEXT NOT NULL,
			name TEXT NOT NULL,
			prompt TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant, name)
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create personalities table: %w", err)
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_personalities_tenant ON personalities(tenant)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create personalities index: %w", err)
	}
	return &PersonalityStore{
		db:     db,
		active: make(map[string]string),
		seeded: make(map[string]bool),
	}, nil
}

// Close closes the database.
func (ps *PersonalityStore) Close() error { return ps.db.Close() }

// normName lowercases and trims a persona name so '/personality Concise' and
// '/personality concise' resolve to the same entry.
func normName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func selKey(tenant, session string) string { return tenant + "\x00" + session }

// SeedDefaults inserts the built-in personas for a tenant if they are not
// already present. Idempotent: existing rows (including tenant edits) are left
// untouched. Runs at most once per tenant per process.
func (ps *PersonalityStore) SeedDefaults(tenant string) error {
	if tenant == "" {
		return fmt.Errorf("personality: tenant is required")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.seeded[tenant] {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, p := range DefaultPersonalities {
		// INSERT OR IGNORE keeps any tenant-customized prompt for the same name.
		if _, err := ps.db.Exec(
			`INSERT OR IGNORE INTO personalities (tenant, name, prompt, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			tenant, normName(p.Name), p.Prompt, now, now,
		); err != nil {
			return fmt.Errorf("seed personality %q: %w", p.Name, err)
		}
	}
	ps.seeded[tenant] = true
	return nil
}

// Save creates or updates a persona for a tenant (CRUD: create/update).
func (ps *PersonalityStore) Save(p Personality) error {
	if p.Tenant == "" {
		return fmt.Errorf("personality: tenant is required")
	}
	name := normName(p.Name)
	if name == "" {
		return fmt.Errorf("personality: name is required")
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return fmt.Errorf("personality: prompt is required")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := ps.db.Exec(`
		INSERT INTO personalities (tenant, name, prompt, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant, name) DO UPDATE SET prompt=excluded.prompt, updated_at=excluded.updated_at
	`, p.Tenant, name, p.Prompt, now, now)
	return err
}

// Get returns a single persona by (tenant, name). Returns ErrUnknownPersonality
// (wrapped) when there is no such persona in the tenant's catalog.
func (ps *PersonalityStore) Get(tenant, name string) (Personality, error) {
	if tenant == "" {
		return Personality{}, fmt.Errorf("personality: tenant is required")
	}
	n := normName(name)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	row := ps.db.QueryRow(
		"SELECT tenant, name, prompt, created_at, updated_at FROM personalities WHERE tenant=? AND name=?",
		tenant, n,
	)
	var p Personality
	var created, updated string
	if err := row.Scan(&p.Tenant, &p.Name, &p.Prompt, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Personality{}, fmt.Errorf("%w: %q (tenant %q)", ErrUnknownPersonality, n, tenant)
		}
		return Personality{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return p, nil
}

// List returns every persona for a tenant, name-sorted. Tenant-scoped: never
// crosses a tenant boundary, so each tenant has an isolated catalog.
func (ps *PersonalityStore) List(tenant string) ([]Personality, error) {
	if tenant == "" {
		return []Personality{}, nil
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	rows, err := ps.db.Query(
		"SELECT tenant, name, prompt, created_at, updated_at FROM personalities WHERE tenant=? ORDER BY name ASC",
		tenant,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Personality{}
	for rows.Next() {
		var p Personality
		var created, updated string
		if err := rows.Scan(&p.Tenant, &p.Name, &p.Prompt, &created, &updated); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Delete removes a persona from a tenant's catalog.
func (ps *PersonalityStore) Delete(tenant, name string) error {
	if tenant == "" {
		return fmt.Errorf("personality: tenant is required")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	_, err := ps.db.Exec("DELETE FROM personalities WHERE tenant=? AND name=?", tenant, normName(name))
	return err
}

// Select marks a persona active for (tenant, session). The persona must exist in
// the tenant's catalog; an unknown name returns ErrUnknownPersonality and leaves
// the prior selection (if any) unchanged. The selection persists for the life of
// the session (process), so subsequent turns compose with the same persona.
func (ps *PersonalityStore) Select(tenant, session, name string) (Personality, error) {
	p, err := ps.Get(tenant, name)
	if err != nil {
		return Personality{}, err
	}
	ps.selMu.Lock()
	ps.active[selKey(tenant, session)] = p.Name
	ps.selMu.Unlock()
	return p, nil
}

// ActiveName returns the persona name currently selected for (tenant, session),
// or "" when none has been selected.
func (ps *PersonalityStore) ActiveName(tenant, session string) string {
	ps.selMu.RLock()
	defer ps.selMu.RUnlock()
	return ps.active[selKey(tenant, session)]
}

// ClearSelection drops any active selection for a session (e.g. on /clear).
func (ps *PersonalityStore) ClearSelection(tenant, session string) {
	ps.selMu.Lock()
	delete(ps.active, selKey(tenant, session))
	ps.selMu.Unlock()
}

// ActivePrompt returns the system-prompt fragment of the persona currently
// active for (tenant, session). Returns "" (no error) when no persona is
// selected, or when the selected persona was deleted out from under the session
// — composition must never fail a chat, so a stale selection degrades to no
// fragment rather than erroring.
func (ps *PersonalityStore) ActivePrompt(tenant, session string) string {
	name := ps.ActiveName(tenant, session)
	if name == "" {
		return ""
	}
	p, err := ps.Get(tenant, name)
	if err != nil {
		personalityLogger.Warn("active personality missing from catalog", map[string]any{
			"tenant": tenant, "name": name,
		})
		return ""
	}
	return p.Prompt
}

// Compose prepends the selected persona's prompt to an existing system-context
// prelude (e.g. the per-workspace context-file loader output, user profile, and
// long-term memory). The persona frames everything below it. When no persona is
// active the prelude is returned unchanged, so this is a safe drop-in around the
// existing prelude assembly. The seedTenant ensures defaults exist before the
// first selection in a tenant.
func (ps *PersonalityStore) Compose(tenant, session, existingPrelude string) string {
	persona := ps.ActivePrompt(tenant, session)
	if persona == "" {
		return existingPrelude
	}
	block := "## Personality\n\n" + strings.TrimRight(persona, "\n")
	if strings.TrimSpace(existingPrelude) == "" {
		return block
	}
	return block + "\n\n" + existingPrelude
}

// PersonalityNames lists just the persona names for a tenant (for help/listing).
func (ps *PersonalityStore) PersonalityNames(tenant string) []string {
	list, err := ps.List(tenant)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

// ParsePersonalityCommand detects a leading '/personality <name>' command in a
// chat message. ok is true when the message is the command; name is the
// requested persona (empty when the user typed bare '/personality', which should
// list the catalog). Recognizes '/persona' as a short alias. Non-command
// messages return ok=false so normal chat flows through unchanged.
func ParsePersonalityCommand(message string) (name string, ok bool) {
	trimmed := strings.TrimSpace(message)
	for _, prefix := range []string{"/personality", "/persona"} {
		if trimmed == prefix {
			return "", true
		}
		if strings.HasPrefix(trimmed, prefix+" ") {
			return strings.TrimSpace(trimmed[len(prefix):]), true
		}
	}
	return "", false
}

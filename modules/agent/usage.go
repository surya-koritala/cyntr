package agent

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// UsageRecord tracks token usage for a single LLM call.
type UsageRecord struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Tenant       string    `json:"tenant"`
	Agent        string    `json:"agent"`
	Provider     string    `json:"provider"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	DurationMs   int64     `json:"duration_ms"`
}

// UsageSummary aggregates usage over a time period.
type UsageSummary struct {
	Tenant        string  `json:"tenant"`
	Agent         string  `json:"agent"`
	Provider      string  `json:"provider"`
	TotalCalls    int     `json:"total_calls"`
	TotalTokens   int     `json:"total_tokens"`
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
}

// UsageStore persists usage records to SQLite.
type UsageStore struct {
	mu sync.Mutex
	db *sql.DB
}

func NewUsageStore(dbPath string) (*UsageStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open usage db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS usage (
		id TEXT PRIMARY KEY,
		timestamp TEXT NOT NULL,
		tenant TEXT NOT NULL,
		agent TEXT NOT NULL,
		provider TEXT NOT NULL,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create usage table: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_tenant_agent ON usage(tenant, agent)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create index: %w", err)
	}

	return &UsageStore{db: db}, nil
}

func (s *UsageStore) Record(rec UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec.ID == "" {
		rec.ID = fmt.Sprintf("usage_%d", time.Now().UnixNano())
	}

	_, err := s.db.Exec(
		`INSERT INTO usage (id, timestamp, tenant, agent, provider, input_tokens, output_tokens, total_tokens, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Timestamp.UTC().Format(time.RFC3339),
		rec.Tenant, rec.Agent, rec.Provider,
		rec.InputTokens, rec.OutputTokens, rec.TotalTokens, rec.DurationMs,
	)
	return err
}

func (s *UsageStore) Query(tenant, agent string, since, until time.Time) ([]UsageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `SELECT id, timestamp, tenant, agent, provider, input_tokens, output_tokens, total_tokens, duration_ms
			  FROM usage WHERE 1=1`
	var args []any

	if tenant != "" {
		query += " AND tenant = ?"
		args = append(args, tenant)
	}
	if agent != "" {
		query += " AND agent = ?"
		args = append(args, agent)
	}
	if !since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	if !until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, until.UTC().Format(time.RFC3339))
	}
	query += " ORDER BY timestamp DESC LIMIT 1000"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []UsageRecord
	for rows.Next() {
		var r UsageRecord
		var ts string
		rows.Scan(&r.ID, &ts, &r.Tenant, &r.Agent, &r.Provider,
			&r.InputTokens, &r.OutputTokens, &r.TotalTokens, &r.DurationMs)
		r.Timestamp, _ = time.Parse(time.RFC3339, ts)
		records = append(records, r)
	}
	if records == nil {
		records = []UsageRecord{}
	}
	return records, rows.Err()
}

func (s *UsageStore) Summarize(tenant string) ([]UsageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `SELECT tenant, agent, provider,
			  COUNT(*) as calls,
			  SUM(total_tokens) as tokens,
			  SUM(input_tokens) as input_tok,
			  SUM(output_tokens) as output_tok,
			  AVG(duration_ms) as avg_dur
			  FROM usage`
	var args []any
	if tenant != "" {
		query += " WHERE tenant = ?"
		args = append(args, tenant)
	}
	query += " GROUP BY tenant, agent, provider ORDER BY tokens DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []UsageSummary
	for rows.Next() {
		var s UsageSummary
		rows.Scan(&s.Tenant, &s.Agent, &s.Provider, &s.TotalCalls,
			&s.TotalTokens, &s.InputTokens, &s.OutputTokens, &s.AvgDurationMs)
		summaries = append(summaries, s)
	}
	if summaries == nil {
		summaries = []UsageSummary{}
	}
	return summaries, rows.Err()
}

func (s *UsageStore) Close() error { return s.db.Close() }

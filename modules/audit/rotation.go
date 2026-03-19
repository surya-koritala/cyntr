package audit

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// RotatingWriter wraps Writer with daily database rotation.
type RotatingWriter struct {
	mu       sync.Mutex
	dir      string
	instance string
	secret   string
	current  *Writer
	curDate  string
}

// NewRotatingWriter creates a rotating audit writer.
// Each day gets its own SQLite file: <dir>/audit-YYYY-MM-DD.db
func NewRotatingWriter(dir, instance, secret string) (*RotatingWriter, error) {
	rw := &RotatingWriter{dir: dir, instance: instance, secret: secret}
	if err := rw.rotate(); err != nil {
		return nil, err
	}
	return rw, nil
}

func (rw *RotatingWriter) Write(entry Entry) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if today != rw.curDate {
		if err := rw.rotateLocked(); err != nil {
			return fmt.Errorf("rotate: %w", err)
		}
	}

	return rw.current.Write(entry)
}

func (rw *RotatingWriter) VerifyChain() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.current == nil {
		return fmt.Errorf("no active writer")
	}
	return rw.current.VerifyChain()
}

func (rw *RotatingWriter) DB() interface{} {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.current != nil {
		return rw.current.db
	}
	return nil
}

func (rw *RotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.current != nil {
		return rw.current.Close()
	}
	return nil
}

func (rw *RotatingWriter) rotate() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.rotateLocked()
}

func (rw *RotatingWriter) rotateLocked() error {
	if rw.current != nil {
		rw.current.Close()
	}

	today := time.Now().UTC().Format("2006-01-02")
	dbPath := filepath.Join(rw.dir, fmt.Sprintf("audit-%s.db", today))

	w, err := NewWriter(dbPath, rw.instance, rw.secret)
	if err != nil {
		return err
	}

	rw.current = w
	rw.curDate = today
	return nil
}

// CurrentPath returns the path of the current day's database.
func (rw *RotatingWriter) CurrentPath() string {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	return filepath.Join(rw.dir, fmt.Sprintf("audit-%s.db", today))
}

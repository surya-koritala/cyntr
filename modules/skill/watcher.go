package skill

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Watcher monitors a directory for skill changes and triggers reloads.
type Watcher struct {
	mu       sync.Mutex
	dir      string
	registry *Registry
	interval time.Duration
	stop     chan struct{}
	stopped  chan struct{}
	lastMod  map[string]time.Time // path -> last modified time
	onChange func(name string)    // callback when a skill is reloaded
}

// NewWatcher creates a file watcher for skills.
func NewWatcher(dir string, registry *Registry, interval time.Duration) *Watcher {
	if interval == 0 {
		interval = 5 * time.Second
	}
	return &Watcher{
		dir:      dir,
		registry: registry,
		interval: interval,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
		lastMod:  make(map[string]time.Time),
	}
}

// SetOnChange sets a callback for when a skill is reloaded.
func (w *Watcher) SetOnChange(fn func(string)) { w.onChange = fn }

// Start begins watching for changes.
func (w *Watcher) Start() {
	go w.watchLoop()
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.stop)
	<-w.stopped
}

func (w *Watcher) watchLoop() {
	defer close(w.stopped)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Initial scan
	w.scan()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

func (w *Watcher) scan() {
	w.mu.Lock()
	defer w.mu.Unlock()

	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(w.dir, entry.Name())
		manifestPath := filepath.Join(skillDir, "skill.yaml")

		info, err := os.Stat(manifestPath)
		if err != nil {
			continue
		}

		modTime := info.ModTime()
		lastMod, seen := w.lastMod[skillDir]

		if !seen {
			// New skill — install
			w.lastMod[skillDir] = modTime
			w.installOrUpdate(skillDir)
		} else if modTime.After(lastMod) {
			// Modified — reload
			w.lastMod[skillDir] = modTime
			w.installOrUpdate(skillDir)
		}
	}
}

func (w *Watcher) installOrUpdate(dir string) {
	skill, err := LoadSkill(dir)
	if err != nil {
		return
	}

	// Remove old version if exists (ignore error when not found)
	_ = w.registry.Uninstall(skill.Manifest.Name)
	_ = w.registry.InstallDirect(skill)

	if w.onChange != nil {
		w.onChange(skill.Manifest.Name)
	}
}

// WatchedCount returns number of tracked skill directories.
func (w *Watcher) WatchedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.lastMod)
}

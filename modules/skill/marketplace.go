package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/netguard"
)

// MarketplaceEntry represents a skill available in the marketplace.
type MarketplaceEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	DownloadURL string `json:"download_url"`
	Downloads   int    `json:"downloads"`
	Source      string `json:"source,omitempty"` // "builtin", "github", or ""
}

// Marketplace is a client for a remote skill registry.
type Marketplace struct {
	baseURL string
	client  *http.Client
}

// NewMarketplace creates a marketplace client.
func NewMarketplace(baseURL string) *Marketplace {
	if baseURL == "" {
		baseURL = "https://registry.cyntr.dev"
	}
	return &Marketplace{
		baseURL: baseURL,
		// Use the SSRF-guarded client so redirects to internal addresses are
		// re-validated for every request the marketplace makes.
		client: netguard.GuardedHTTPClient(30 * time.Second),
	}
}

// safeJoin joins name onto base, guaranteeing the result stays within base.
// It rejects absolute paths and any ".." traversal.
func safeJoin(base, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty name")
	}
	cleaned := filepath.Clean("/" + name) // force relative, collapse ".."
	joined := filepath.Join(base, cleaned)
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	if absJoined != absBase && !strings.HasPrefix(absJoined, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return joined, nil
}

// Search finds skills matching a query.
func (m *Marketplace) Search(ctx context.Context, query string) ([]MarketplaceEntry, error) {
	reqURL := fmt.Sprintf("%s/api/v1/skills/search?q=%s", m.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketplace search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return nil, fmt.Errorf("marketplace error %d: %s", resp.StatusCode, string(body))
	}

	var entries []MarketplaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil && err != io.EOF {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	if entries == nil {
		entries = []MarketplaceEntry{}
	}
	return entries, nil
}

// Get returns details for a specific skill.
func (m *Marketplace) Get(ctx context.Context, name string) (*MarketplaceEntry, error) {
	reqURL := fmt.Sprintf("%s/api/v1/skills/%s", m.baseURL, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	var entry MarketplaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decode skill response: %w", err)
	}
	return &entry, nil
}

// Download fetches a skill package and saves it to the given directory.
func (m *Marketplace) Download(ctx context.Context, entry MarketplaceEntry, destDir string) (string, error) {
	if entry.DownloadURL == "" {
		return "", fmt.Errorf("no download URL for %s", entry.Name)
	}

	// The download URL is registry/model-supplied; guard against SSRF before
	// fetching (rejects loopback/link-local/private/metadata targets).
	if err := netguard.ValidatePublicURL(entry.DownloadURL); err != nil {
		return "", fmt.Errorf("reject download URL for %s: %w", entry.Name, err)
	}

	// Confine the destination to destDir: the registry-supplied name must not
	// escape via path separators or "..".
	skillDir, err := safeJoin(destDir, entry.Name)
	if err != nil {
		return "", fmt.Errorf("invalid skill name %q: %w", entry.Name, err)
	}

	client := m.client
	if client == nil {
		client = netguard.GuardedHTTPClient(30 * time.Second)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", entry.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return "", fmt.Errorf("create skill dir: %w", err)
	}

	// Save the skill manifest
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	manifestPath := filepath.Join(skillDir, "skill.yaml")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}

	return skillDir, nil
}

// List returns all available skills.
func (m *Marketplace) List(ctx context.Context) ([]MarketplaceEntry, error) {
	return m.Search(ctx, "")
}

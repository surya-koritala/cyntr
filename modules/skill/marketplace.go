package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
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
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Search finds skills matching a query.
func (m *Marketplace) Search(ctx context.Context, query string) ([]MarketplaceEntry, error) {
	url := fmt.Sprintf("%s/api/v1/skills/search?q=%s", m.baseURL, query)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	json.NewDecoder(resp.Body).Decode(&entries)
	if entries == nil {
		entries = []MarketplaceEntry{}
	}
	return entries, nil
}

// Get returns details for a specific skill.
func (m *Marketplace) Get(ctx context.Context, name string) (*MarketplaceEntry, error) {
	url := fmt.Sprintf("%s/api/v1/skills/%s", m.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	json.NewDecoder(resp.Body).Decode(&entry)
	return &entry, nil
}

// Download fetches a skill package and saves it to the given directory.
func (m *Marketplace) Download(ctx context.Context, entry MarketplaceEntry, destDir string) (string, error) {
	if entry.DownloadURL == "" {
		return "", fmt.Errorf("no download URL for %s", entry.Name)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", entry.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	skillDir := filepath.Join(destDir, entry.Name)
	os.MkdirAll(skillDir, 0755)

	// Save the skill manifest
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	manifestPath := filepath.Join(skillDir, "skill.yaml")
	os.WriteFile(manifestPath, data, 0644)

	return skillDir, nil
}

// List returns all available skills.
func (m *Marketplace) List(ctx context.Context) ([]MarketplaceEntry, error) {
	return m.Search(ctx, "")
}

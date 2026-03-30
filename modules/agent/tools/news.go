package tools

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// NewsAggregatorTool fetches and caches real news from RSS feeds.
// Agents use this to get current, sourced, real-world content for posting.
type NewsAggregatorTool struct {
	client    *http.Client
	mu        sync.RWMutex
	cache     []NewsItem
	consumed  map[string]bool // track returned article links to avoid duplicates
	lastFetch time.Time
	cacheTTL  time.Duration
}

// NewsItem represents a single news article from an RSS feed.
type NewsItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Source      string `json:"source"`
	Category    string `json:"category"`
	PubDate     string `json:"pub_date"`
}

// RSS feed sources mapped to categories
var newsSources = map[string][]struct {
	Name string
	URL  string
}{
	"world-news": {
		{"BBC World", "https://feeds.bbci.co.uk/news/world/rss.xml"},
		{"Guardian World", "https://www.theguardian.com/world/rss"},
	},
	"science": {
		{"BBC Science", "https://feeds.bbci.co.uk/news/science_and_environment/rss.xml"},
		{"NYT Science", "https://rss.nytimes.com/services/xml/rss/nyt/Science.xml"},
		{"Ars Science", "https://feeds.arstechnica.com/arstechnica/science"},
		{"Phys.org", "https://phys.org/rss-feed/"},
	},
	"economics": {
		{"BBC Business", "https://feeds.bbci.co.uk/news/business/rss.xml"},
	},
	"health": {
		{"BBC Health", "https://feeds.bbci.co.uk/news/health/rss.xml"},
		{"NYT Health", "https://rss.nytimes.com/services/xml/rss/nyt/Health.xml"},
	},
	"climate": {
		{"NYT Climate", "https://rss.nytimes.com/services/xml/rss/nyt/Climate.xml"},
	},
	"education": {
		{"BBC Education", "https://feeds.bbci.co.uk/news/education/rss.xml"},
	},
	"gaming": {
		{"Ars Gaming", "https://feeds.arstechnica.com/arstechnica/gaming"},
		{"PCGamer", "https://www.pcgamer.com/rss/"},
		{"RPS", "https://www.rockpapershotgun.com/feed"},
		{"GameDev", "https://www.gamedeveloper.com/rss.xml"},
	},
	"startups": {
		{"The Verge", "https://www.theverge.com/rss/index.xml"},
	},
	"hardware": {
		{"Ars Gadgets", "https://arstechnica.com/gadgets/feed/"},
	},
	"privacy": {
		{"EFF", "https://www.eff.org/rss/updates.xml"},
		{"Schneier", "https://www.schneier.com/feed/"},
		{"Krebs", "https://krebsonsecurity.com/feed/"},
	},
	"technology": {
		{"BBC Tech", "https://feeds.bbci.co.uk/news/technology/rss.xml"},
		{"NYT Tech", "https://rss.nytimes.com/services/xml/rss/nyt/Technology.xml"},
		{"Wired", "https://www.wired.com/feed/rss"},
		{"Ars Technica", "https://arstechnica.com/feed/"},
	},
	"security": {
		{"EFF", "https://www.eff.org/rss/updates.xml"},
		{"Krebs", "https://krebsonsecurity.com/feed/"},
	},
	"devops": {
		{"Lobsters", "https://lobste.rs/rss"},
	},
	"general": {
		{"Guardian Science", "https://www.theguardian.com/science/rss"},
	},
}

var imgPattern = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)
var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func NewNewsAggregatorTool() *NewsAggregatorTool {
	return &NewsAggregatorTool{
		client:   &http.Client{Timeout: 15 * time.Second},
		consumed: make(map[string]bool),
		cacheTTL: 30 * time.Minute,
	}
}

func (t *NewsAggregatorTool) Name() string { return "news_aggregator" }
func (t *NewsAggregatorTool) Description() string {
	return "Fetch real current news from RSS feeds. Returns articles with titles, links, images, and summaries from BBC, NYT, Guardian, Ars Technica, and more. Categories: world-news, science, economics, health, climate, education, gaming, startups, hardware, privacy, technology, security, devops, general."
}

func (t *NewsAggregatorTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"category": {Type: "string", Description: "Category: world-news, science, economics, health, climate, education, gaming, startups, hardware, privacy, technology, security, devops, general. Use 'all' for everything.", Required: true},
		"limit":    {Type: "string", Description: "Number of articles to return (default 5, max 20)", Required: false},
	}
}

func (t *NewsAggregatorTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	category := input["category"]
	if category == "" {
		return "", fmt.Errorf("category is required")
	}

	// Refresh cache if stale
	t.mu.RLock()
	stale := time.Since(t.lastFetch) > t.cacheTTL
	t.mu.RUnlock()

	if stale {
		t.refresh()
	}

	// Filter by category, skip already-consumed articles
	t.mu.Lock()
	defer t.mu.Unlock()

	var results []NewsItem
	for _, item := range t.cache {
		if (category == "all" || item.Category == category) && !t.consumed[item.Link] {
			results = append(results, item)
		}
	}

	limit := 5
	if l := input["limit"]; l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit > 20 {
		limit = 20
	}
	if len(results) > limit {
		results = results[:limit]
	}

	// Mark returned articles as consumed so other agents get different ones
	for _, item := range results {
		t.consumed[item.Link] = true
	}

	if len(results) == 0 {
		return fmt.Sprintf("No articles found for category %q. Available categories: world-news, science, economics, health, climate, education, gaming, startups, hardware, privacy, technology, security, devops, general", category), nil
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return string(out), nil
}

func (t *NewsAggregatorTool) refresh() {
	var allItems []NewsItem
	var mu sync.Mutex
	var wg sync.WaitGroup

	for category, feeds := range newsSources {
		for _, feed := range feeds {
			wg.Add(1)
			go func(cat, name, url string) {
				defer wg.Done()
				items := t.fetchFeed(cat, name, url)
				mu.Lock()
				allItems = append(allItems, items...)
				mu.Unlock()
			}(category, feed.Name, feed.URL)
		}
	}
	wg.Wait()

	t.mu.Lock()
	t.cache = allItems
	t.consumed = make(map[string]bool) // reset consumed on refresh
	t.lastFetch = time.Now()
	t.mu.Unlock()
}

func (t *NewsAggregatorTool) fetchFeed(category, sourceName, url string) []NewsItem {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CyntrBot/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB max
	if err != nil {
		return nil
	}

	return t.parseRSS(category, sourceName, body)
}

type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string      `xml:"title"`
	Link        string      `xml:"link"`
	Description string      `xml:"description"`
	PubDate     string      `xml:"pubDate"`
	Enclosure   rssEnclosure `xml:"enclosure"`
	MediaThumb  rssMedia    `xml:"thumbnail"`
	MediaContent rssMedia   `xml:"content"`
}

type rssEnclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type rssMedia struct {
	URL string `xml:"url,attr"`
}

func (t *NewsAggregatorTool) parseRSS(category, sourceName string, data []byte) []NewsItem {
	var root rssRoot
	var items []NewsItem

	if xml.Unmarshal(data, &root) == nil {
		for _, item := range root.Channel.Items {
			if len(items) >= 20 {
				break
			}

			// Find image
			img := item.MediaThumb.URL
			if img == "" {
				img = item.MediaContent.URL
			}
			if img == "" && strings.Contains(item.Enclosure.Type, "image") {
				img = item.Enclosure.URL
			}
			if img == "" {
				if m := imgPattern.FindStringSubmatch(item.Description); len(m) > 1 {
					img = m[1]
				}
			}

			// Clean description
			desc := htmlTagPattern.ReplaceAllString(item.Description, "")
			desc = html.UnescapeString(strings.TrimSpace(desc))
			if len(desc) > 300 {
				desc = desc[:300] + "..."
			}

			title := html.UnescapeString(strings.TrimSpace(item.Title))
			if title == "" || item.Link == "" {
				continue
			}

			items = append(items, NewsItem{
				Title:       title,
				Link:        item.Link,
				Description: desc,
				Image:       img,
				Source:       sourceName,
				Category:    category,
				PubDate:     item.PubDate,
			})
		}
	}

	// Try Atom format if RSS parsing got nothing
	if len(items) == 0 {
		items = t.parseAtom(category, sourceName, data)
	}

	return items
}

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string    `xml:"title"`
	Link    atomLink  `xml:"link"`
	Summary string    `xml:"summary"`
	Content string    `xml:"content"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

func (t *NewsAggregatorTool) parseAtom(category, sourceName string, data []byte) []NewsItem {
	var feed atomFeed
	var items []NewsItem

	if xml.Unmarshal(data, &feed) == nil {
		for _, entry := range feed.Entries {
			if len(items) >= 20 {
				break
			}

			desc := htmlTagPattern.ReplaceAllString(entry.Summary, "")
			desc = html.UnescapeString(strings.TrimSpace(desc))
			if len(desc) > 300 {
				desc = desc[:300] + "..."
			}

			img := ""
			if m := imgPattern.FindStringSubmatch(entry.Content); len(m) > 1 {
				img = m[1]
			}

			title := html.UnescapeString(strings.TrimSpace(entry.Title))
			if title == "" || entry.Link.Href == "" {
				continue
			}

			items = append(items, NewsItem{
				Title:       title,
				Link:        entry.Link.Href,
				Description: desc,
				Image:       img,
				Source:       sourceName,
				Category:    category,
			})
		}
	}
	return items
}

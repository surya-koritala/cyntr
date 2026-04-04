package tools

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math/rand"
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

// RSS feed sources mapped to categories — 170+ verified feeds across 26 categories
var newsSources = map[string][]struct {
	Name string
	URL  string
}{
	"world-news": {
		{"BBC World", "https://feeds.bbci.co.uk/news/world/rss.xml"},
		{"Al Jazeera", "https://www.aljazeera.com/xml/rss/all.xml"},
		{"Guardian World", "https://www.theguardian.com/world/rss"},
		{"NPR World", "https://feeds.npr.org/1004/rss.xml"},
		{"DW News", "https://rss.dw.com/rdf/rss-en-all"},
		{"Independent World", "https://www.independent.co.uk/news/world/rss"},
		{"Japan Times", "https://www.japantimes.co.jp/feed/"},
		{"Global Voices", "https://globalvoices.org/feed/"},
		{"The Diplomat", "https://thediplomat.com/feed/"},
		{"Axios", "https://api.axios.com/feed/top"},
		{"CBC World", "https://rss.cbc.ca/lineup/world.xml"},
		{"UN News", "https://news.un.org/feed/subscribe/en/news/all/rss.xml"},
		{"Middle East Eye", "https://www.middleeasteye.net/rss"},
	},
	"science": {
		{"Nature", "https://www.nature.com/nature.rss"},
		{"Science Daily", "https://www.sciencedaily.com/rss/all.xml"},
		{"New Scientist", "https://www.newscientist.com/feed/home"},
		{"Ars Science", "https://feeds.arstechnica.com/arstechnica/science"},
		{"Live Science", "https://www.livescience.com/feeds/all"},
		{"Science News", "https://www.sciencenews.org/feed"},
		{"Quanta Magazine", "https://quantamagazine.org/feed/"},
		{"Phys.org", "https://phys.org/rss-feed/"},
		{"Nautilus", "https://nautil.us/feed/"},
		{"Science Alert", "https://www.sciencealert.com/feed"},
		{"Futurism", "https://futurism.com/feed"},
		{"Wired Science", "https://www.wired.com/feed/category/science/latest/rss"},
		{"Smithsonian Science", "https://www.smithsonianmag.com/rss/science-nature/"},
		{"Popular Science", "https://www.popsci.com/feed/"},
	},
	"economics": {
		{"BBC Business", "https://feeds.bbci.co.uk/news/business/rss.xml"},
		{"Naked Capitalism", "https://www.nakedcapitalism.com/feed"},
		{"Marginal Revolution", "https://marginalrevolution.com/feed"},
		{"NPR Economy", "https://feeds.npr.org/1017/rss.xml"},
		{"Calculated Risk", "https://www.calculatedriskblog.com/feeds/posts/default?alt=rss"},
		{"EPI Blog", "https://www.epi.org/blog/feed/"},
		{"Farnam Street", "https://fs.blog/feed/"},
		{"Vox Policy", "https://www.vox.com/rss/policy/index.xml"},
		{"CEPR", "https://cepr.net/feed/"},
		{"ProMarket", "https://www.promarket.org/feed/"},
	},
	"health": {
		{"BBC Health", "https://feeds.bbci.co.uk/news/health/rss.xml"},
		{"NPR Health", "https://feeds.npr.org/1128/rss.xml"},
		{"STAT News", "https://www.statnews.com/feed/"},
		{"KFF Health News", "https://kffhealthnews.org/feed/"},
		{"Science Daily Health", "https://www.sciencedaily.com/rss/health_medicine.xml"},
		{"Guardian Health", "https://www.theguardian.com/lifeandstyle/health-and-wellbeing/rss"},
		{"Vox Health", "https://www.vox.com/rss/health-care/index.xml"},
		{"Science Based Medicine", "https://sciencebasedmedicine.org/feed/"},
		{"Undark", "https://undark.org/feed/"},
		{"WHO News", "https://www.who.int/rss-feeds/news-english.xml"},
	},
	"climate": {
		{"NYT Climate", "https://rss.nytimes.com/services/xml/rss/nyt/Climate.xml"},
		{"Carbon Brief", "https://www.carbonbrief.org/feed/"},
		{"Guardian Climate", "https://www.theguardian.com/environment/climate-crisis/rss"},
		{"Inside Climate", "https://insideclimatenews.org/feed/"},
		{"CleanTechnica", "https://cleantechnica.com/feed/"},
	},
	"education": {
		{"BBC Education", "https://feeds.bbci.co.uk/news/education/rss.xml"},
		{"Inside Higher Ed", "https://www.insidehighered.com/rss/feed"},
		{"EdSurge", "https://www.edsurge.com/rss"},
	},
	"gaming": {
		{"Ars Gaming", "https://feeds.arstechnica.com/arstechnica/gaming"},
		{"PCGamer", "https://www.pcgamer.com/rss/"},
		{"RPS", "https://www.rockpapershotgun.com/feed"},
		{"GameDev", "https://www.gamedeveloper.com/rss.xml"},
		{"Eurogamer", "https://www.eurogamer.net/feed"},
		{"Kotaku", "https://kotaku.com/rss"},
	},
	"startups": {
		{"The Verge", "https://www.theverge.com/rss/index.xml"},
		{"TechCrunch", "https://techcrunch.com/feed/"},
		{"Hacker News Best", "https://hnrss.org/best"},
		{"Indie Hackers", "https://www.indiehackers.com/feed.xml"},
	},
	"hardware": {
		{"Ars Gadgets", "https://arstechnica.com/gadgets/feed/"},
		{"Tom's Hardware", "https://www.tomshardware.com/feeds/all"},
		{"IEEE Spectrum", "https://spectrum.ieee.org/feeds/feed.rss"},
		{"Hackaday", "https://hackaday.com/feed/"},
	},
	"privacy": {
		{"EFF", "https://www.eff.org/rss/updates.xml"},
		{"Schneier", "https://www.schneier.com/feed/"},
		{"Krebs", "https://krebsonsecurity.com/feed/"},
		{"The Record", "https://therecord.media/feed/"},
		{"Dark Reading", "https://www.darkreading.com/rss.xml"},
	},
	"technology": {
		{"BBC Tech", "https://feeds.bbci.co.uk/news/technology/rss.xml"},
		{"Wired", "https://www.wired.com/feed/rss"},
		{"Ars Technica", "https://arstechnica.com/feed/"},
		{"MIT Tech Review", "https://www.technologyreview.com/feed/"},
		{"Rest of World", "https://restofworld.org/feed/"},
	},
	"security": {
		{"EFF", "https://www.eff.org/rss/updates.xml"},
		{"Krebs", "https://krebsonsecurity.com/feed/"},
		{"Bleeping Computer", "https://www.bleepingcomputer.com/feed/"},
	},
	"devops": {
		{"Lobsters", "https://lobste.rs/rss"},
		{"CNCF Blog", "https://www.cncf.io/blog/feed/"},
		{"The New Stack", "https://thenewstack.io/feed/"},
	},
	"space": {
		{"NASA", "https://www.nasa.gov/news-release/feed/"},
		{"Space.com", "https://www.space.com/feeds/all"},
		{"SpaceNews", "https://spacenews.com/feed/"},
		{"Ars Space", "https://arstechnica.com/space/feed/"},
	},
	"ai": {
		{"MIT AI", "https://www.technologyreview.com/topic/artificial-intelligence/feed/"},
		{"AI News", "https://www.artificialintelligence-news.com/feed/"},
		{"VentureBeat AI", "https://venturebeat.com/category/ai/feed/"},
		{"The Batch", "https://www.deeplearning.ai/the-batch/feed/"},
	},
	"robotics": {
		{"IEEE Robotics", "https://spectrum.ieee.org/topic/robotics/feed.rss"},
		{"The Robot Report", "https://www.therobotreport.com/feed/"},
		{"Hackaday", "https://hackaday.com/feed/"},
	},
	"biotech": {
		{"STAT News", "https://www.statnews.com/feed/"},
		{"Endpoints News", "https://endpts.com/feed/"},
		{"Nature Biotech", "https://www.nature.com/nbt.rss"},
	},
	"finance": {
		{"Bloomberg", "https://feeds.bloomberg.com/markets/news.rss"},
		{"Finextra", "https://www.finextra.com/rss/headlines.aspx"},
		{"Coin Telegraph", "https://cointelegraph.com/rss"},
		{"MarketWatch", "https://feeds.marketwatch.com/marketwatch/topstories/"},
	},
	"environment": {
		{"Guardian Environment", "https://www.theguardian.com/environment/rss"},
		{"Mongabay", "https://news.mongabay.com/feed/"},
		{"Yale E360", "https://e360.yale.edu/feed"},
		{"Treehugger", "https://www.treehugger.com/rss"},
	},
	"sports": {
		{"ESPN Top", "https://www.espn.com/espn/rss/news"},
		{"ESPN NFL", "https://www.espn.com/espn/rss/nfl/news"},
		{"ESPN NBA", "https://www.espn.com/espn/rss/nba/news"},
		{"ESPN Soccer", "https://www.espn.com/espn/rss/soccer/news"},
		{"ESPN F1", "https://www.espn.com/espn/rss/rpm/news"},
		{"BBC Sport", "https://feeds.bbci.co.uk/sport/rss.xml"},
		{"BBC Football", "https://feeds.bbci.co.uk/sport/football/rss.xml"},
		{"BBC Cricket", "https://feeds.bbci.co.uk/sport/cricket/rss.xml"},
		{"BBC Tennis", "https://feeds.bbci.co.uk/sport/tennis/rss.xml"},
		{"BBC F1", "https://feeds.bbci.co.uk/sport/formula1/rss.xml"},
		{"Guardian Sport", "https://www.theguardian.com/uk/sport/rss"},
		{"SB Nation", "https://www.sbnation.com/rss/index.xml"},
		{"Yahoo Sports", "https://sports.yahoo.com/rss/"},
		{"Deadspin", "https://deadspin.com/rss"},
		{"Cycling News", "https://www.cyclingnews.com/rss"},
	},
	"culture": {
		{"Guardian Culture", "https://www.theguardian.com/culture/rss"},
		{"NPR Arts", "https://feeds.npr.org/1048/rss.xml"},
		{"NPR Books", "https://feeds.npr.org/1032/rss.xml"},
		{"The Atlantic", "https://www.theatlantic.com/feed/all/"},
		{"Aeon", "https://aeon.co/feed.rss"},
		{"Hyperallergic", "https://hyperallergic.com/feed/"},
		{"Pitchfork", "https://pitchfork.com/feed/feed-news/rss"},
		{"ArtNews", "https://www.artnews.com/feed/"},
		{"Literary Hub", "https://lithub.com/feed/"},
		{"Open Culture", "https://www.openculture.com/feed"},
		{"Colossal", "https://www.thisiscolossal.com/feed/"},
		{"Longreads", "https://longreads.com/feed/"},
	},
	"life": {
		{"Aeon", "https://aeon.co/feed.rss"},
		{"The Marginalian", "https://www.themarginalian.org/feed/"},
		{"Psyche", "https://psyche.co/feed"},
		{"Daily Stoic", "https://dailystoic.com/feed/"},
		{"Farnam Street", "https://fs.blog/feed/"},
		{"3 Quarks Daily", "https://3quarksdaily.com/feed"},
		{"Ribbonfarm", "https://www.ribbonfarm.com/feed/"},
		{"BigThink", "https://bigthink.com/feed/"},
	},
	"food": {
		{"Food Safety News", "https://www.foodsafetynews.com/feed/"},
		{"Civil Eats", "https://civileats.com/feed/"},
		{"Modern Farmer", "https://modernfarmer.com/feed/"},
		{"Eater", "https://www.eater.com/rss/index.xml"},
		{"Food Politics", "https://www.foodpolitics.com/feed/"},
		{"AgFunder News", "https://agfundernews.com/feed"},
		{"Food Tank", "https://foodtank.com/feed/"},
		{"Guardian Food", "https://www.theguardian.com/lifeandstyle/food-and-drink/rss"},
		{"NPR Food", "https://feeds.npr.org/1053/rss.xml"},
		{"The Takeout", "https://thetakeout.com/rss"},
	},
	"history": {
		{"Smithsonian History", "https://www.smithsonianmag.com/rss/history/"},
		{"Atlas Obscura", "https://www.atlasobscura.com/feeds/latest"},
		{"History Extra", "https://www.historyextra.com/feed/"},
		{"All That's Interesting", "https://allthatsinteresting.com/feed"},
		{"Medievalists", "https://www.medievalists.net/feed/"},
		{"Ancient Origins", "https://www.ancient-origins.net/rss.xml"},
		{"The History Blog", "https://www.thehistoryblog.com/feed"},
		{"Public Domain Review", "https://publicdomainreview.org/rss.xml"},
		{"JSTOR Daily", "https://daily.jstor.org/feed/"},
		{"Archaeology Magazine", "https://www.archaeology.org/feed"},
	},
	"psychology": {
		{"PsyPost", "https://www.psypost.org/feed/"},
		{"Neuroscience News", "https://neurosciencenews.com/feed/"},
		{"Behavioral Scientist", "https://behavioralscientist.org/feed/"},
		{"Greater Good Berkeley", "https://greatergood.berkeley.edu/rss/all"},
		{"BigThink", "https://bigthink.com/feed/"},
		{"Science Daily Mind", "https://www.sciencedaily.com/rss/mind_brain.xml"},
		{"PsyBlog", "https://www.spring.org.uk/feed"},
	},
	"general": {
		{"Guardian Science", "https://www.theguardian.com/science/rss"},
		{"NPR News", "https://feeds.npr.org/1001/rss.xml"},
		{"Smithsonian", "https://www.smithsonianmag.com/rss/latest_articles/"},
		{"Atlas Obscura", "https://www.atlasobscura.com/feeds/latest"},
		{"JSTOR Daily", "https://daily.jstor.org/feed/"},
	},
}

var imgPattern = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)
var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func NewNewsAggregatorTool() *NewsAggregatorTool {
	return &NewsAggregatorTool{
		client:   &http.Client{Timeout: 15 * time.Second},
		consumed: make(map[string]bool),
		cacheTTL: 10 * time.Minute,
	}
}

func (t *NewsAggregatorTool) Name() string { return "fetch_news" }
func (t *NewsAggregatorTool) Description() string {
	return "Fetch real current news from RSS feeds. Returns articles with titles, links, images, and summaries from 90+ sources. Categories: world-news, science, economics, health, climate, education, gaming, startups, hardware, privacy, technology, security, devops, space, ai, robotics, biotech, finance, environment, sports, culture, life, food, history, psychology, general."
}

func (t *NewsAggregatorTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"category": {Type: "string", Description: "Category: world-news, science, economics, health, climate, education, gaming, startups, hardware, privacy, technology, security, devops, space, ai, robotics, biotech, finance, environment, sports, culture, life, food, history, psychology, general. Use 'all' for everything.", Required: true},
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

	// Filter by category, skip already-consumed articles, shuffle for diversity
	t.mu.Lock()
	defer t.mu.Unlock()

	var results []NewsItem
	for _, item := range t.cache {
		if (category == "all" || item.Category == category) && !t.consumed[item.Link] {
			results = append(results, item)
		}
	}

	// Shuffle so each agent gets different articles from the pool
	rand.Shuffle(len(results), func(i, j int) {
		results[i], results[j] = results[j], results[i]
	})

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

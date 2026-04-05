package tools

// AgentAssignment maps each agent to their dedicated community and news sources.
// This prevents multiple agents from posting about the same topic.
// Each agent ONLY posts to their assigned community using their assigned sources.
var AgentAssignments = map[string]struct {
	Community  string
	Categories []string // news categories this agent can pull from
}{
	// World News — 4 agents, each with different source focus
	"atlas":      {Community: "world-news", Categories: []string{"world-news"}},
	"spark":      {Community: "world-news", Categories: []string{"world-news"}},
	"skywalker":  {Community: "world-news", Categories: []string{"world-news"}},

	// Science — 3 agents
	"hermes":     {Community: "science", Categories: []string{"science"}},
	"sage":       {Community: "science", Categories: []string{"science"}},

	// Climate & Energy — 2 agents
	"prometheus": {Community: "climate", Categories: []string{"climate"}},
	"cipher":     {Community: "climate", Categories: []string{"climate", "environment"}},

	// Health — 2 agents
	"oracle":     {Community: "health", Categories: []string{"health"}},
	"prism":      {Community: "health", Categories: []string{"health", "biotech"}},

	// Privacy & Security — 2 agents
	"drift":      {Community: "privacy", Categories: []string{"privacy"}},
	"flux":       {Community: "security", Categories: []string{"security"}},

	// Hardware — 2 agents
	"forge":      {Community: "hardware", Categories: []string{"hardware"}},
	"zenith":     {Community: "hardware", Categories: []string{"hardware"}},

	// Startups & Business — 2 agents
	"nova":       {Community: "startups", Categories: []string{"startups"}},
	"bond007":    {Community: "startups", Categories: []string{"startups", "finance"}},

	// Gaming — 2 agents
	"raven":      {Community: "gaming", Categories: []string{"gaming"}},
	"blaze":      {Community: "gaming", Categories: []string{"gaming"}},

	// Space — 2 agents
	"beacon":     {Community: "space", Categories: []string{"space"}},
	"apollo":     {Community: "space", Categories: []string{"space"}},

	// AI News — 2 agents
	"nexus":      {Community: "ai-news", Categories: []string{"ai"}},
	"pulse":      {Community: "ai-news", Categories: []string{"ai", "technology"}},

	// AI Safety — 2 agents
	"athena":     {Community: "ai-safety", Categories: []string{"ai"}},
	"core":       {Community: "ai-safety", Categories: []string{"ai", "technology"}},

	// Robotics — 2 agents
	"volt":       {Community: "robotics", Categories: []string{"robotics"}},
	"anubis":     {Community: "robotics", Categories: []string{"robotics", "hardware"}},

	// Biotech — 2 agents
	"artemis":    {Community: "biotech", Categories: []string{"biotech"}},
	"bastet":     {Community: "biotech", Categories: []string{"biotech", "health"}},

	// Finance — 2 agents
	"yoda":       {Community: "finance", Categories: []string{"finance"}},
	"arcturus":   {Community: "finance", Categories: []string{"finance", "economics"}},

	// Environment — 2 agents
	"aurora":     {Community: "environment", Categories: []string{"environment"}},
	"breeze":     {Community: "environment", Categories: []string{"environment", "climate"}},

	// Economics — 2 agents
	"echo":       {Community: "economics", Categories: []string{"economics"}},
	"ares":       {Community: "economics", Categories: []string{"economics", "finance"}},

	// DevOps — 2 agents
	"argon":      {Community: "devops", Categories: []string{"devops"}},
	"aria":       {Community: "devops", Categories: []string{"devops", "technology"}},

	// Education — 1 agent
	"cadence":    {Community: "education", Categories: []string{"education"}},

	// Code Review — 1 agent
	"altair":     {Community: "code-review", Categories: []string{"devops", "technology"}},

	// Machine Learning — 1 agent
	"antares":    {Community: "machine-learning", Categories: []string{"ai"}},

	// Frameworks — 1 agent
	"baldr":      {Community: "frameworks", Categories: []string{"devops", "technology"}},

	// Careers — 1 agent
	"amber":      {Community: "careers", Categories: []string{"economics", "technology"}},

	// Sports — 2 agents
	"calypso":    {Community: "sports", Categories: []string{"sports"}},
	"corvus":     {Community: "sports", Categories: []string{"sports"}},

	// Culture — 2 agents
	"cygnus":     {Community: "culture", Categories: []string{"culture"}},
	"deneb":      {Community: "culture", Categories: []string{"culture"}},

	// Life & Philosophy — 1 agent
	"draco":      {Community: "life", Categories: []string{"life"}},

	// Food — 1 agent
	"hydra":      {Community: "food", Categories: []string{"food"}},

	// History — 1 agent
	"lupus":      {Community: "history", Categories: []string{"history"}},

	// Psychology — 1 agent
	"pyxis":      {Community: "psychology", Categories: []string{"psychology"}},

	// Open Forum (original thought) — 2 agents
	"aquila":     {Community: "open-forum", Categories: []string{"general"}},
	"ceres":      {Community: "open-forum", Categories: []string{"general"}},

	// Debates — 1 agent
	"io":         {Community: "debates", Categories: []string{"general"}},

	// General — 1 agent
	"ganymede":   {Community: "general", Categories: []string{"general"}},

	// OSAI — 1 agent
	"europa":     {Community: "osai", Categories: []string{"devops", "technology"}},
}

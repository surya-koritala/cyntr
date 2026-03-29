# Multi-Agent Alatirok Social Experiment — Plan

## Overview

**6 AI agents** on Alatirok, each with a distinct model and personality, creating content, debating, voting, and building community — orchestrated by Cyntr.

## Agents

| Agent | Model | Alatirok Name | Personality | API Key |
|-------|-------|--------------|-------------|---------|
| Cyntr OS | claude-haiku-4-5 | Cyntr Agent OS | Platform representative | ak_b88face... |
| Veteran | gpt-4.1 | The Veteran | Cautious, analytical, cites sources | ak_352c01... |
| Thinker | gpt-5-chat | The Thinker | Philosophical, big picture | ak_56de87... |
| Challenger | gpt-5.2-chat | The Challenger | Contrarian, questions assumptions | ak_5b0e2f... |
| Synthesizer | gpt-5.4-mini | The Synthesizer | Fast, summarizes, finds consensus | ak_83c258... |
| Scout | gpt-5.4-nano | The Scout | Searches web, brings news | ak_ce4f56... |

## Azure OpenAI Config
- Endpoint: `https://roamx-resource.cognitiveservices.azure.com`
- Deployments: `gpt-4.1`, `gpt-5-chat`, `gpt-5.2-chat`, `gpt-5.4-mini`, `gpt-5.4-nano`
- API Version: `2025-04-01-preview`
- Note: Use `max_completion_tokens` not `max_tokens`

## Alatirok Platform Findings

**Working post types:** text, link, alert, question, debate
**Not working:** research, meta, data (return "invalid post_type")
**Communities:** osai, security, ml, ai-safety, frameworks (all have 10 subs)
**Agent policy:** osai/ml/frameworks = "open", security/ai-safety = "verified"
**Community creation:** works (agents can create via JWT)
**Community delete:** not supported (405 Method Not Allowed)
**Agent registration:** requires JWT (human owner), not API key

## Execution Plan

### Phase 1: Infrastructure Setup
1. Create an Azure OpenAI provider in Cyntr that uses the roamx endpoint
2. Register 5 Cyntr agents (one per model) with alatirok tool + web_search tool
3. Store all Alatirok API keys in .env
4. Create a new community on Alatirok: "AI Agents in the Wild" for meta-discussion

### Phase 2: Scout Brings News
1. Scout agent web-searches for current AI news (last 7 days)
2. Scout posts 3-5 news items across communities (osai, ml, frameworks)
3. Post type: "link" with summary

### Phase 3: Debate Topic
1. Thinker creates a "debate" post: "Should AI agents have persistent identity and reputation scores?"
2. Veteran comments with evidence-based position (cites existing systems)
3. Challenger comments with contrarian view
4. Synthesizer reads all comments and posts a summary
5. All agents vote on each other's comments

### Phase 4: Cross-Community Activity
1. Veteran posts in security: "AI-powered threat detection: current state of the art"
2. Challenger posts in ai-safety: "Why agent reputation systems might do more harm than good"
3. Thinker posts in frameworks: philosophical take on agent architectures
4. Agents comment on each other's posts across communities

### Phase 5: Feedback Collection
After all interactions, document:
- What API calls failed or returned unexpected results
- What features are missing (edit post? delete post? subscribe to community? notifications?)
- What would make the agent experience better
- UX/API design suggestions for Alatirok

## Platform Feedback to Collect

Things to test and report:
- [ ] Can agents edit their own posts?
- [ ] Can agents delete their own posts?
- [ ] Can agents subscribe to communities?
- [ ] Do agents get notifications on replies?
- [ ] Can agents see who voted on their posts?
- [ ] Is there a rate limit? What happens when hit?
- [ ] Can agents mention other agents?
- [ ] Are there any content moderation filters?
- [ ] What happens with very long posts?
- [ ] Can agents create polls or structured content?
- [ ] Does search work well for agent-generated content?
- [ ] Can agents have threaded conversations (nested replies)?

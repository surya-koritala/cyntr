#!/bin/bash
# Alatirok Discussion Engine — runs as a cron job
# Agents read posts, understand context, and have real conversations
# Usage: ./scripts/alatirok_discussion.sh [post|discuss|both]

CYNTR_URL="http://localhost:7700"
ALATIROK_URL="https://www.alatirok.com"
MODE="${1:-both}"

AGENTS=("atlas" "oracle" "prometheus" "athena" "hermes" "cipher" "forge" "sage" "beacon" "echo" "nova" "raven" "nexus" "pulse" "volt" "prism" "spark" "flux" "core" "drift")
NEWS_SITES=("https://news.ycombinator.com/" "https://www.reuters.com/technology/" "https://www.bbc.com/news" "https://techcrunch.com/")

# Get all community IDs
get_communities() {
  curl -s --max-time 10 "$ALATIROK_URL/api/v1/communities" | python3 -c "
import json,sys
for c in json.load(sys.stdin):
    print(c['id'])
" 2>/dev/null
}

# Clear agent session for fresh context
clear_session() {
  curl -s -X DELETE "$CYNTR_URL/api/v1/tenants/test-live/agents/$1/sessions/sess_test-live_$1" > /dev/null 2>&1
}

# Post: agent researches + creates a post (checks for duplicates first)
do_post() {
  local AGENT=$1
  local COMM=$2
  local NEWS=${NEWS_SITES[$((RANDOM % 4))]}

  clear_session "$AGENT"
  curl -s -X POST "$CYNTR_URL/api/v1/tenants/test-live/agents/$AGENT/chat" \
    -H 'Content-Type: application/json' \
    -d "{\"message\": \"STEP 1: Use alatirok get_feed sort=new limit=30. Read ALL the titles carefully.

STEP 2: Use http_request GET $NEWS. Find ONE story that has NOT already been posted (check against the titles from step 1).

STEP 3: Use alatirok create_post. community_id=$COMM. Pick any post_type. Include the source URL. Write a UNIQUE catchy title that is different from every title you saw in step 1. Proper markdown with ## headings and bullet points. For debates include metadata with position_a and position_b.

CRITICAL: If a similar topic already exists on the feed, pick a COMPLETELY DIFFERENT story. No variations of existing posts.\", \"user\": \"cron\"}" \
    --max-time 120 > /dev/null 2>&1
}

# Discuss: agent reads a post + comments + replies to existing thread
do_discuss() {
  local AGENT=$1
  local SORT=$2  # rotate between new, hot, top to spread engagement

  clear_session "$AGENT"
  curl -s -X POST "$CYNTR_URL/api/v1/tenants/test-live/agents/$AGENT/chat" \
    -H 'Content-Type: application/json' \
    -d "{\"message\": \"Follow these steps carefully:

STEP 1: Use alatirok get_feed sort=$SORT limit=20.

STEP 2: SKIP posts that already have more than 15 comments — they have enough discussion. Pick a post with 0-5 comments that needs engagement.

STEP 3: Use alatirok get_post with that post_id to read the FULL content.

STEP 4: Use alatirok get_comments with the same post_id to read any existing comments.

STEP 5: Write a comment that DIRECTLY ENGAGES with the post:
- Reference specific claims or arguments from the post
- If there are existing comments, respond to a specific one using parent_comment_id
- Add new information, evidence, or a different perspective
- Do NOT write generic responses

STEP 6: Pick ANOTHER post with few comments (under 5). Read it. Comment on it.

STEP 7: Pick a THIRD post with few comments. Read it. Comment on it.

STEP 8: Vote on 5 posts and 3 comments.

CRITICAL: Do NOT keep commenting on the same popular posts. Find posts with LOW comment counts that need discussion.\", \"user\": \"cron\"}" \
    --max-time 300 > /dev/null 2>&1
}

# Run posting cycle
run_posts() {
  echo "$(date): Starting post cycle"
  COMMS=($(get_communities))

  for i in $(seq 0 9); do
    AGENT=${AGENTS[$((RANDOM % 20))]}
    COMM=${COMMS[$((RANDOM % ${#COMMS[@]}))]}
    do_post "$AGENT" "$COMM" &
    sleep 3
  done
  wait
  echo "$(date): 10 posts submitted"
}

# Run discussion cycle
run_discussions() {
  echo "$(date): Starting discussion cycle"
  SORTS=("new" "hot" "top")

  # Batch 1: 10 agents discuss (different sort orders to find different posts)
  for i in $(seq 0 9); do
    AGENT=${AGENTS[$i]}
    SORT=${SORTS[$((i % 3))]}
    do_discuss "$AGENT" "$SORT" &
    sleep 2
  done
  wait
  echo "$(date): Batch 1 (10 agents) discussed"

  # Batch 2: other 10 agents discuss
  for i in $(seq 10 19); do
    AGENT=${AGENTS[$i]}
    SORT=${SORTS[$((i % 3))]}
    do_discuss "$AGENT" "$SORT" &
    sleep 2
  done
  wait
  echo "$(date): Batch 2 (10 agents) discussed"
}

# Report status
report() {
  TOTAL=0
  for S in $(curl -s --max-time 10 "$ALATIROK_URL/api/v1/communities" | python3 -c "import json,sys; [print(c['slug']) for c in json.load(sys.stdin)]" 2>/dev/null); do
    C=$(curl -s --max-time 5 "$ALATIROK_URL/api/v1/communities/$S/feed?sort=new&limit=50" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('total', len(d.get('data',[]))))" 2>/dev/null)
    TOTAL=$((TOTAL + C))
  done

  curl -s --max-time 10 "$ALATIROK_URL/api/v1/feed?sort=hot&limit=100" | python3 -c "
import json,sys; d=json.load(sys.stdin).get('data',[])
v = sum(p.get('vote_score',0) for p in d)
c = sum(p.get('comment_count',0) for p in d)
print(f'Posts: $TOTAL | Votes: {v}+ | Comments: {c}+')
" 2>/dev/null
}

# Main
case "$MODE" in
  post)
    run_posts
    report
    ;;
  discuss)
    run_discussions
    report
    ;;
  both)
    run_posts
    run_discussions
    report
    ;;
  *)
    echo "Usage: $0 [post|discuss|both]"
    exit 1
    ;;
esac

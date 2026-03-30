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

# Post: agent researches + creates a post
do_post() {
  local AGENT=$1
  local COMM=$2
  local NEWS=${NEWS_SITES[$((RANDOM % 4))]}

  clear_session "$AGENT"
  curl -s -X POST "$CYNTR_URL/api/v1/tenants/test-live/agents/$AGENT/chat" \
    -H 'Content-Type: application/json' \
    -d "{\"message\": \"Use http_request GET $NEWS. Find ONE interesting story. Then use alatirok create_post. community_id=$COMM. Pick any post_type. Include the source URL. Catchy title. Proper markdown with ## headings and bullet points. For debates include metadata with position_a and position_b.\", \"user\": \"cron\"}" \
    --max-time 120 > /dev/null 2>&1
}

# Discuss: agent reads a post + comments + replies to existing thread
do_discuss() {
  local AGENT=$1

  clear_session "$AGENT"
  curl -s -X POST "$CYNTR_URL/api/v1/tenants/test-live/agents/$AGENT/chat" \
    -H 'Content-Type: application/json' \
    -d '{"message": "Follow these steps carefully:

STEP 1: Use alatirok get_feed sort=hot limit=15. Pick the post with the most comments.

STEP 2: Use alatirok get_post with that post_id to read the FULL post content.

STEP 3: Use alatirok get_comments with the same post_id to read ALL existing comments and replies.

STEP 4: Now write a comment that DIRECTLY ENGAGES with the post content AND the existing discussion:
- If you agree with someone, say WHY and add new evidence
- If you disagree, explain your reasoning and reference their specific argument
- If you see a gap in the discussion, point it out
- Quote or reference specific commenters by name when responding to them
- Use parent_comment_id to make it a threaded reply to the most relevant comment

STEP 5: Pick a DIFFERENT post from the feed. Read it with get_post. Read its comments with get_comments. Write another contextual comment.

STEP 6: Vote on 5 posts and 3 comments — upvote insightful content, downvote low-effort or generic responses.

IMPORTANT: Do NOT write generic responses like Great post! or Interesting analysis! Every comment must reference specific points from the post or other comments.", "user": "cron"}' \
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

  # Batch 1: 10 agents discuss
  for i in $(seq 0 9); do
    AGENT=${AGENTS[$i]}
    do_discuss "$AGENT" &
    sleep 2
  done
  wait
  echo "$(date): Batch 1 (10 agents) discussed"

  # Batch 2: other 10 agents discuss
  for i in $(seq 10 19); do
    AGENT=${AGENTS[$i]}
    do_discuss "$AGENT" &
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

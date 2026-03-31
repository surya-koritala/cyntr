#!/bin/bash
# Quality Discussion Engine — agents read posts deeply, discuss, vote epistemic status
# Usage: ./scripts/alatirok_discuss.sh

CYNTR_URL="http://localhost:7700"
AGENTS=("atlas" "hermes" "prometheus" "oracle" "cipher" "forge" "sage" "nova" "raven" "beacon" "athena" "prism" "volt" "spark" "drift" "nexus" "pulse" "flux" "core" "echo")

discuss() {
  local A=$1
  local SORT=$2

  curl -s -X DELETE "$CYNTR_URL/api/v1/tenants/test-live/agents/$A/sessions/sess_test-live_$A" > /dev/null 2>&1
  curl -s -X POST "$CYNTR_URL/api/v1/tenants/test-live/agents/$A/chat" \
    -H 'Content-Type: application/json' \
    -d "{\"message\": \"You are having a genuine discussion on Alatirok. Follow these steps:

STEP 1: Use alatirok get_feed sort=$SORT limit=20. Find a post with FEWER than 10 comments that looks interesting.

STEP 2: Use alatirok get_post with that post_id. READ THE ENTIRE POST carefully — every paragraph, every claim, every source link.

STEP 3: Use alatirok get_comments with the same post_id. Read ALL existing comments.

STEP 4: Write a GENUINE comment using alatirok create_comment on that post_id:
- Reference SPECIFIC claims from the post (quote or paraphrase them)
- If there are existing comments, respond to a specific commenter using parent_comment_id
- Add NEW information the post missed — background context, counterexamples, related developments
- If you agree, explain WHY with evidence
- If you disagree, explain your reasoning specifically
- Ask a follow-up question that pushes the discussion deeper
- Keep it 2-3 paragraphs max

STEP 5: Evaluate the post epistemic status. Use alatirok epistemic_vote on the same post_id:
- hypothesis — if the post makes unverified claims without sources
- supported — if the post has credible sources and evidence
- contested — if the topic is genuinely debatable with valid arguments on both sides
- consensus — if the post states widely accepted facts

STEP 6: Vote on the post using alatirok vote (target_type=post, direction=up if well-written, down if low-effort)

STEP 7: Find a SECOND post with few comments. Repeat steps 2-6 for it.

STEP 8: Find a THIRD post. Repeat.

ABSOLUTE RULES:
- Do NOT write generic comments like Great analysis or Interesting post
- Do NOT comment on posts you have already commented on
- Every comment must reference specific content from the post
- Every epistemic vote must be justified by the post content\", \"user\": \"autopilot\"}" \
    --max-time 300 > /dev/null 2>&1
}

SORTS=("new" "hot" "top" "new" "hot")

echo "$(date): Starting discussion cycle"

# Batch 1: 10 agents
for i in $(seq 0 9); do
  discuss "${AGENTS[$i]}" "${SORTS[$((i % 5))]}" &
  sleep 2
done
wait
echo "$(date): Batch 1 done (10 agents)"

# Batch 2: other 10 agents
for i in $(seq 10 19); do
  discuss "${AGENTS[$i]}" "${SORTS[$((i % 5))]}" &
  sleep 2
done
wait
echo "$(date): Batch 2 done (10 agents)"

T=$(curl -s --max-time 10 "https://www.alatirok.com/api/v1/feed?sort=hot&limit=100" | python3 -c "
import json,sys; d=json.load(sys.stdin).get('data',[])
print(f'Votes: {sum(p.get(\"vote_score\",0) for p in d)}+ | Comments: {sum(p.get(\"comment_count\",0) for p in d)}+')
" 2>/dev/null)
echo "$(date): $T"

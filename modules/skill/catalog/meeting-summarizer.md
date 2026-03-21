---
name: meeting-summarizer
description: Extract decisions, action items, and structured summaries from meeting notes
version: 1.0.0
author: cyntr
tools:
  - name: file_read
  - name: file_write
  - name: knowledge_search
  - name: knowledge_store
---

# Meeting Summarizer

Process raw meeting notes or transcripts to extract decisions, action items,
open questions, and produce a structured summary suitable for distribution.

## Prerequisites

- The user must provide meeting notes via one of:
  - A file path (read with `file_read`)
  - Direct text input in the conversation
  - A reference to notes in the knowledge base (search with `knowledge_search`)
- Notes can be in any format: raw transcript, bullet points, free-form text.

## Step 1: Read and Parse Input

If given a file path, read the full contents with `file_read`.

Determine the format of the notes:
- **Transcript format**: Includes speaker labels (e.g., "Alice:", "[Alice]",
  "Speaker 1:"). Parse each utterance with its speaker.
- **Bullet point format**: Structured as a bulleted or numbered list.
- **Free-form format**: Paragraph text without clear structure.
- **Timestamped format**: Includes timestamps (e.g., "10:05 AM", "[00:15:30]").

Extract metadata if present:
- **Meeting title**: Usually the first line or a heading.
- **Date**: Look for date patterns in the first 5 lines.
- **Attendees**: Look for "Attendees:", "Present:", "Participants:" followed by
  a list of names.
- **Duration**: If timestamps are present, compute from first to last timestamp.

If metadata is missing, ask the user to provide it or mark as "Not specified."

## Step 2: Identify Topics Discussed

Segment the notes into topics. Use these signals to detect topic boundaries:

1. **Explicit headers**: Lines starting with `#`, `##`, or all-caps text.
2. **Speaker transitions**: When a new speaker introduces a new subject.
3. **Timestamp gaps**: If timestamps show a gap of more than 5 minutes between
   entries, likely a topic change.
4. **Transition phrases**: "Moving on to", "Next item", "Let's discuss",
   "Regarding", "On the topic of".
5. **Agenda items**: If an agenda was provided, map discussion sections to
   agenda items.

For each topic, record:
- Topic title (inferred from content if not explicitly stated)
- Start and end timestamps (if available)
- Duration (if timestamps available)
- Key speakers for that topic

## Step 3: Extract Decisions

Scan the notes for statements indicating decisions. Look for patterns:

- "We decided to...", "The decision is...", "We agreed..."
- "We'll go with...", "Let's do...", "The plan is..."
- "Approved:", "Rejected:", "Tabled:"
- "Going forward, we will..."
- "The consensus is..."
- Statements following "vote" or "all in favor"

For each decision, record:
- **Decision text**: The specific decision made, stated clearly.
- **Context**: What topic or problem the decision addresses.
- **Decider**: Who made or announced the decision (if identifiable).
- **Dissent**: Any noted disagreements or conditions.

If no clear decisions are found, state "No explicit decisions identified" and
list any statements that might be implicit decisions for the user to confirm.

## Step 4: Extract Action Items

Scan for commitments and assignments. Look for patterns:

- "<Name> will...", "<Name> to...", "<Name> is going to..."
- "Action item:", "TODO:", "Follow-up:"
- "Can you <do something>?" followed by affirmative response
- "I'll take care of...", "I'll handle..."
- "By <date>", "Before next meeting", "This week", "EOD"
- "@<name>" mentions followed by tasks

For each action item, record:
- **Task**: Clear description of what needs to be done.
- **Owner**: The person responsible. If unclear, mark as "TBD" and flag for
  the user to assign.
- **Deadline**: Extracted date or relative timeframe. If no deadline mentioned,
  mark as "No deadline set."
- **Priority**: Inferred from language ("urgent", "critical", "when you get a
  chance", "nice to have").
- **Status**: Default to "Pending."

## Step 5: Extract Open Questions

Identify questions that were raised but not resolved during the meeting:

- Questions followed by "We'll figure that out later" or "Let's table that"
- Questions without a clear answer in the subsequent discussion
- Statements like "We still need to determine...", "TBD", "Open question"
- "Does anyone know...?" without a definitive answer

For each open question, record:
- **Question**: The specific question.
- **Context**: Why it was raised and what decision it blocks.
- **Suggested owner**: Who should follow up (if mentioned).

## Step 6: Compute Time Allocation (if timestamps available)

If the notes include timestamps, calculate:

- Total meeting duration
- Time spent per topic (from topic start to next topic start)
- Percentage of total time per topic

Sort topics by time spent (descending). Flag if any single topic consumed more
than 50% of the meeting time.

## Step 7: Generate Structured Summary

Format the output:

```
# Meeting Summary: <Title>
**Date:** <date>
**Duration:** <duration or "Not recorded">
**Attendees:** <comma-separated list or "Not specified">

## Key Takeaways
- <1-sentence summary of the most important outcome>
- <Second most important outcome>
- <Third most important outcome>

## Decisions Made
| # | Decision | Context | Decided By |
|---|----------|---------|------------|
| 1 | <text>   | <text>  | <name>     |

## Action Items
| # | Task | Owner | Deadline | Priority |
|---|------|-------|----------|----------|
| 1 | <text> | <name> | <date> | <High/Medium/Low> |

## Open Questions
| # | Question | Context | Follow-up Owner |
|---|----------|---------|-----------------|
| 1 | <text>   | <text>  | <name or TBD>   |

## Discussion Summary

### <Topic 1> <(duration if available)>
<2-4 sentence summary of what was discussed and concluded>

### <Topic 2>
<2-4 sentence summary>

...

## Time Allocation
| Topic | Duration | % of Meeting |
|-------|----------|--------------|
<rows, sorted by duration descending>

---
*Summarized by cyntr/meeting-summarizer v1.0.0*
```

## Step 8: Store in Knowledge Base

If the `knowledge_store` tool is available, store the summary for future
reference:

- Store with tags: `meeting`, `<date>`, `<topic keywords>`
- Store the full structured summary
- Store action items separately for easy retrieval

This enables future queries like "What did we decide about X?" or "What are
John's open action items?"

## Step 9: Output

If the user specified an output file, write with `file_write`. Suggest filename:
`meeting-summary-<date>-<title-slug>.md`

If the user wants a short version (e.g., for Slack), produce an abbreviated
format:
- Decisions as a bullet list
- Action items as a numbered list with owners
- Skip the detailed discussion summary

Return the complete summary as the response.

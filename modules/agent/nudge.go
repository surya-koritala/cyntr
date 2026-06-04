package agent

// Self-nudge to persist knowledge (A4).
//
// When a long conversation is about to have its older context compacted away,
// the agent injects a short system reminder asking itself to save anything
// durable (a user fact, preference, decision, or reusable procedure) before it
// is lost. This is a prompt-level mechanism — the model decides whether to act
// by calling its memory / user-model tools.
//
// Rate limiting is structural rather than counter-based: compaction only fires
// when history exceeds the summarize threshold, and afterwards history shrinks
// to threshold/2, so a fresh nudge can only recur after roughly threshold/2
// more turns. That keeps nudges frequent enough to matter on long sessions and
// infrequent enough to never spam the model.

// nudgeSystemMessage is the reminder injected just before compaction.
func nudgeSystemMessage() Message {
	return Message{
		Role:    RoleSystem,
		Content: "Before older messages are compacted away: if anything worth remembering for future conversations emerged — a durable fact about the user, a preference, a decision, or a reusable procedure — save it now using your memory or user-model tools. If nothing is worth saving, just continue.",
	}
}

// nudgeBeforeCompact injects the self-nudge, then compacts history keeping the
// most recent keepRecent messages. The nudge is added last so it survives
// compaction and reaches the model on its next turn.
func nudgeBeforeCompact(s *Session, keepRecent int) {
	s.AddMessage(nudgeSystemMessage())
	s.CompactHistory(keepRecent)
}

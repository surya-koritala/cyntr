// Package learn implements the closed learning loop (A1): after a complex
// task it reflects on the completed turn and produces durable improvements —
// a long-term memory and, when a reusable procedure emerged, a proposed skill.
//
// It consumes both Sprint 0 foundations: it is triggered by the
// agent.turn_completed event (F0.1) and does its LLM work on the background
// job queue (F0.2), so the user's chat is never blocked. It is gated off by
// default (CYNTR_LEARN_ENABLED) because it spends tokens and writes durable
// state.
package learn

import "context"

// JobKindReflect is the kernel/jobs kind for a reflection job.
const JobKindReflect = "learn.reflect"

// DefaultMinToolCalls is the complexity threshold: a turn with at least this
// many tool invocations is considered worth reflecting on.
const DefaultMinToolCalls = 3

// ReflectFunc runs the after-action review prompt against an LLM and returns
// its raw response. Wired to a model provider in main.go; nil disables
// reflection.
type ReflectFunc func(ctx context.Context, prompt string) (string, error)

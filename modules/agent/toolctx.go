package agent

import "context"

// Context keys for tool execution. Tools that need to enforce policy
// (e.g. tool_plan, which dispatches sub-calls) read these from the context
// the runtime hands them.
type toolCtxKey int

const (
	ctxKeyTenant toolCtxKey = iota
	ctxKeyAgent
	ctxKeyUser
)

// WithToolCaller annotates the context with the calling tenant, agent, and user
// so downstream tool implementations can enforce policy on sub-calls.
func WithToolCaller(ctx context.Context, tenant, agentName, user string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyTenant, tenant)
	ctx = context.WithValue(ctx, ctxKeyAgent, agentName)
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	return ctx
}

// ToolCaller returns the calling tenant, agent, and user from the context.
// Empty strings are returned when the values were not set.
func ToolCaller(ctx context.Context) (tenant, agentName, user string) {
	tenant, _ = ctx.Value(ctxKeyTenant).(string)
	agentName, _ = ctx.Value(ctxKeyAgent).(string)
	user, _ = ctx.Value(ctxKeyUser).(string)
	return
}

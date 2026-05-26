package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// startUserModelBus spins up an IPC bus with a fake usermodel handler that
// returns the supplied profile for every usermodel.get call.
func startUserModelBus(t *testing.T, profile usermodel.UserProfile) *ipc.Bus {
	t.Helper()
	bus := ipc.NewBus()
	bus.Handle("usermodel", usermodel.TopicGet, func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: profile}, nil
	})
	t.Cleanup(func() { bus.Close() })
	return bus
}

func TestUserModelReadName(t *testing.T) {
	if NewUserModelReadTool(nil).Name() != "user_model_read" {
		t.Fatal("wrong name")
	}
}

func TestUserModelReadReturnsLabeledSections(t *testing.T) {
	bus := startUserModelBus(t, usermodel.UserProfile{
		Tenant: "acme", User: "alice",
		ProfileMD:     "Alice loves terse answers.",
		PreferencesMD: "- metric units\n- short bullets",
	})
	tool := NewUserModelReadTool(bus)

	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	out, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "# User profile") {
		t.Fatalf("missing profile header: %s", out)
	}
	if !strings.Contains(out, "Alice loves terse answers") {
		t.Fatalf("missing profile body: %s", out)
	}
	if !strings.Contains(out, "# User preferences") {
		t.Fatalf("missing preferences header: %s", out)
	}
	if !strings.Contains(out, "metric units") {
		t.Fatalf("missing preferences body: %s", out)
	}
}

func TestUserModelReadEmptyProfile(t *testing.T) {
	bus := startUserModelBus(t, usermodel.UserProfile{Tenant: "acme", User: "alice"})
	tool := NewUserModelReadTool(bus)
	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	out, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "empty") {
		t.Fatalf("expected empty marker, got: %s", out)
	}
}

func TestUserModelReadRequiresToolCaller(t *testing.T) {
	bus := startUserModelBus(t, usermodel.UserProfile{})
	tool := NewUserModelReadTool(bus)
	// No WithToolCaller — should fail.
	_, err := tool.Execute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no tenant/user") {
		t.Fatalf("expected tenant/user error, got: %v", err)
	}
}

func TestUserModelReadNoHandlerIsSoftError(t *testing.T) {
	bus := ipc.NewBus()
	t.Cleanup(func() { bus.Close() })
	tool := NewUserModelReadTool(bus)

	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	out, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("ErrNoHandler should not propagate as error, got: %v", err)
	}
	if !strings.Contains(out, "no user model module") {
		t.Fatalf("expected soft-error message, got: %s", out)
	}
}

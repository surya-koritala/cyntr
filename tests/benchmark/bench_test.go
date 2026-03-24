package benchmark

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
)

func BenchmarkToolRegistryLookup(b *testing.B) {
	reg := agent.NewToolRegistry()
	reg.Register(&agenttools.ShellTool{})
	reg.Register(agenttools.NewHTTPTool())
	reg.Register(agenttools.NewJSONQueryTool())
	reg.Register(agenttools.NewCSVQueryTool())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.Get("json_query")
	}
}

func BenchmarkToolRegistryList(b *testing.B) {
	reg := agent.NewToolRegistry()
	for i := 0; i < 30; i++ {
		reg.Register(agenttools.NewJSONQueryTool()) // same tool, different iterations don't matter for list
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.List()
	}
}

func BenchmarkIPCBusRequest(b *testing.B) {
	bus := ipc.NewBus()
	bus.Handle("test", "test.echo", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: msg.Payload}, nil
	})

	ctx := context.Background()
	msg := ipc.Message{Source: "bench", Target: "test", Topic: "test.echo", Payload: "hello"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Request(ctx, msg)
	}
}

func BenchmarkIPCBusPublish(b *testing.B) {
	bus := ipc.NewBus()
	bus.Subscribe("test", "test.event", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{}, nil
	})

	msg := ipc.Message{Source: "bench", Topic: "test.event", Payload: "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(msg)
	}
}

func BenchmarkSessionStoreAppend(b *testing.B) {
	store, _ := agent.NewSessionStore(filepath.Join(b.TempDir(), "bench.db"))
	defer store.Close()

	cfg := agent.AgentConfig{Name: "bench", Tenant: "t"}
	store.SaveSession("sess_bench", cfg)

	msg := agent.Message{Role: agent.RoleUser, Content: "benchmark message"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.AppendMessage("sess_bench", msg)
	}
}

func BenchmarkSessionStoreLoad(b *testing.B) {
	store, _ := agent.NewSessionStore(filepath.Join(b.TempDir(), "bench.db"))
	defer store.Close()

	cfg := agent.AgentConfig{Name: "bench", Tenant: "t"}
	store.SaveSession("sess_bench", cfg)
	for i := 0; i < 50; i++ {
		store.AppendMessage("sess_bench", agent.Message{Role: agent.RoleUser, Content: "msg"})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.LoadSession("sess_bench")
	}
}

func BenchmarkMemoryStoreRecall(b *testing.B) {
	store, _ := agent.NewMemoryStore(filepath.Join(b.TempDir(), "bench_mem.db"))
	defer store.Close()

	for i := 0; i < 20; i++ {
		store.Save(agent.Memory{
			Agent: "bench", Tenant: "t", Key: "fact",
			Content: "This is a memory entry for benchmarking purposes",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Recall("bench", "t")
	}
}

func BenchmarkJSONQueryExecute(b *testing.B) {
	tool := agenttools.NewJSONQueryTool()
	input := map[string]string{
		"json_data": `{"users":[{"name":"Alice","age":30},{"name":"Bob","age":25}],"count":2}`,
		"path":      "users[0].name",
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(ctx, input)
	}
}

func BenchmarkCSVQueryStats(b *testing.B) {
	tool := agenttools.NewCSVQueryTool()
	csv := "name,age,score\n"
	for i := 0; i < 100; i++ {
		csv += "user,25,85\n"
	}
	input := map[string]string{"csv_data": csv, "action": "stats", "column": "score"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(ctx, input)
	}
}

func BenchmarkSecretMasking(b *testing.B) {
	text := "Here is key AKIAIOSFODNN7EXAMPLE and token xoxb-123-456-abc and password=secret123 in the output."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agent.MaskSecrets(text)
	}
}

func BenchmarkUsageStoreRecord(b *testing.B) {
	store, _ := agent.NewUsageStore(filepath.Join(b.TempDir(), "bench_usage.db"))
	defer store.Close()

	rec := agent.UsageRecord{
		Timestamp: time.Now(), Tenant: "bench", Agent: "bot",
		Provider: "claude", TotalTokens: 100, DurationMs: 50,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.ID = ""
		store.Record(rec)
	}
}

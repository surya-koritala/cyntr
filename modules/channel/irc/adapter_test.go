package irc

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestIRCAdapterInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestIRCAdapterName(t *testing.T) {
	if New("irc.example:6667", "bot", []string{"#ops"}, "t", "a").Name() != "irc" {
		t.Fatal("expected irc")
	}
}

func TestNewNormalizesChannels(t *testing.T) {
	a := New("s", "bot", []string{"ops", "#dev", " ", "&local"}, "t", "a")
	want := []string{"#ops", "#dev", "&local"}
	if len(a.channels) != len(want) {
		t.Fatalf("got %v", a.channels)
	}
	for i, c := range want {
		if a.channels[i] != c {
			t.Fatalf("channel %d: got %q want %q", i, a.channels[i], c)
		}
	}
}

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantPrefix string
		wantCmd    string
		wantParams []string
	}{
		{"privmsg channel", ":alice!a@host PRIVMSG #ops :hello world", "alice!a@host", "PRIVMSG", []string{"#ops", "hello world"}},
		{"privmsg dm", ":bob!b@h PRIVMSG cyntr :hi there", "bob!b@h", "PRIVMSG", []string{"cyntr", "hi there"}},
		{"no prefix", "PING :token123", "", "PING", []string{"token123"}},
		{"join", ":x!y@z JOIN #ops", "x!y@z", "JOIN", []string{"#ops"}},
		{"trailing with colon", ":a!b@c PRIVMSG #x :see: this", "a!b@c", "PRIVMSG", []string{"#x", "see: this"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, cmd, params := parseMessage(tt.line)
			if p != tt.wantPrefix {
				t.Errorf("prefix: got %q want %q", p, tt.wantPrefix)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd: got %q want %q", cmd, tt.wantCmd)
			}
			if strings.Join(params, "|") != strings.Join(tt.wantParams, "|") {
				t.Errorf("params: got %v want %v", params, tt.wantParams)
			}
		})
	}
}

func TestNickFromPrefix(t *testing.T) {
	if nickFromPrefix("alice!a@host") != "alice" {
		t.Fatal("expected alice")
	}
	if nickFromPrefix("server.name") != "server.name" {
		t.Fatal("expected passthrough")
	}
}

// fakeServer is a localhost TCP server emulating the bare minimum of an IRC
// server: it records every line the client sends and lets the test push lines
// back to the client.
type fakeServer struct {
	ln    net.Listener
	mu    sync.Mutex
	lines []string
	conn  net.Conn
	ready chan struct{}
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fs := &fakeServer{ln: ln, ready: make(chan struct{})}
	go fs.accept()
	return fs
}

func (fs *fakeServer) accept() {
	conn, err := fs.ln.Accept()
	if err != nil {
		return
	}
	fs.mu.Lock()
	fs.conn = conn
	fs.mu.Unlock()
	close(fs.ready)
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		fs.mu.Lock()
		fs.lines = append(fs.lines, strings.TrimRight(scanner.Text(), "\r"))
		fs.mu.Unlock()
	}
}

func (fs *fakeServer) addr() string { return fs.ln.Addr().String() }

func (fs *fakeServer) push(t *testing.T, line string) {
	t.Helper()
	<-fs.ready
	fs.mu.Lock()
	conn := fs.conn
	fs.mu.Unlock()
	if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
		t.Fatalf("push: %v", err)
	}
}

func (fs *fakeServer) sent() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]string, len(fs.lines))
	copy(out, fs.lines)
	return out
}

func (fs *fakeServer) close() { fs.ln.Close() }

func waitFor(t *testing.T, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestStartRegistersAndJoins(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	a := New(fs.addr(), "cyntr", []string{"#ops", "dev"}, "tenantA", "assistant")
	ctx := context.Background()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)

	ok := waitFor(t, func() bool {
		s := strings.Join(fs.sent(), "\n")
		return strings.Contains(s, "NICK cyntr") &&
			strings.Contains(s, "USER cyntr 0 * :cyntr") &&
			strings.Contains(s, "JOIN #ops") &&
			strings.Contains(s, "JOIN #dev")
	})
	if !ok {
		t.Fatalf("registration/join not observed; sent=%v", fs.sent())
	}
}

func TestInboundPrivmsgCarriesTenantAgent(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	received := make(chan channel.InboundMessage, 1)
	a := New(fs.addr(), "cyntr", []string{"#ops"}, "tenantA", "assistant")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)

	fs.push(t, ":alice!alice@host PRIVMSG #ops :hello cyntr")

	select {
	case msg := <-received:
		if msg.Channel != "irc" {
			t.Errorf("channel: got %q", msg.Channel)
		}
		if msg.Text != "hello cyntr" {
			t.Errorf("text: got %q", msg.Text)
		}
		if msg.ChannelID != "#ops" {
			t.Errorf("channelID: got %q", msg.ChannelID)
		}
		if msg.UserID != "alice" {
			t.Errorf("userID: got %q", msg.UserID)
		}
		if msg.Tenant != "tenantA" {
			t.Errorf("tenant: got %q", msg.Tenant)
		}
		if msg.Agent != "assistant" {
			t.Errorf("agent: got %q", msg.Agent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestInboundDirectMessageRepliesToSender(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	a := New(fs.addr(), "cyntr", nil, "t", "a")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		return "pong", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)

	// DM addressed to our nick => reply target is the sender.
	fs.push(t, ":bob!bob@h PRIVMSG cyntr :ping")

	if !waitFor(t, func() bool {
		return strings.Contains(strings.Join(fs.sent(), "\n"), "PRIVMSG bob :pong")
	}) {
		t.Fatalf("expected reply to sender; sent=%v", fs.sent())
	}
}

func TestPingPong(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	a := New(fs.addr(), "cyntr", nil, "t", "a")
	ctx := context.Background()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)

	fs.push(t, "PING :keepalive123")

	if !waitFor(t, func() bool {
		return strings.Contains(strings.Join(fs.sent(), "\n"), "PONG :keepalive123")
	}) {
		t.Fatalf("expected PONG; sent=%v", fs.sent())
	}
}

func TestSendFormatsPrivmsg(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	a := New(fs.addr(), "cyntr", nil, "t", "a")
	ctx := context.Background()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)

	if err := a.Send(ctx, channel.OutboundMessage{ChannelID: "#ops", Text: "line one\nline two"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	if !waitFor(t, func() bool {
		s := strings.Join(fs.sent(), "\n")
		return strings.Contains(s, "PRIVMSG #ops :line one") && strings.Contains(s, "PRIVMSG #ops :line two")
	}) {
		t.Fatalf("expected two PRIVMSG lines; sent=%v", fs.sent())
	}
}

func TestSendMissingTarget(t *testing.T) {
	a := New("x", "cyntr", nil, "t", "a")
	if err := a.Send(context.Background(), channel.OutboundMessage{Text: "x"}); err == nil {
		t.Fatal("expected error with missing channel ID")
	}
}

func TestSendNotConnected(t *testing.T) {
	a := New("x", "cyntr", nil, "t", "a")
	if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "#ops", Text: "x"}); err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestCleanStartStopNoLeak(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	a := New(fs.addr(), "cyntr", []string{"#ops"}, "t", "a")
	ctx := context.Background()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("start: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Stop(stopCtx); err != nil {
		t.Fatalf("stop returned error (read goroutine may have leaked): %v", err)
	}
	// Second Stop is a no-op.
	if err := a.Stop(stopCtx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestContextCancelStopsReadLoop(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	ctx, cancel := context.WithCancel(context.Background())
	a := New(fs.addr(), "cyntr", nil, "t", "a")
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Cancelling the parent context should let Stop complete promptly.
	cancel()

	stopCtx, c2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer c2()
	if err := a.Stop(stopCtx); err != nil {
		t.Fatalf("stop after ctx cancel: %v", err)
	}
}

// Package irc implements a Channel Manager adapter for IRC (RFC 1459/2812)
// using only the Go standard library. It maintains a persistent TCP
// connection to an IRC server, registers (NICK/USER), JOINs the configured
// channels, replies to server PING with PONG to keep the link alive, and
// translates inbound PRIVMSG lines into channel.InboundMessage carrying the
// configured tenant + agent. Outbound messages are formatted as PRIVMSG.
//
// The adapter is enabled only when configured (see cmd wiring): an empty
// server address means the adapter is never constructed/registered.
package irc

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_irc")

// dialFunc abstracts net.Dial so tests can inject a localhost listener or a
// fake connection. Defaults to a TCP dialer with a connect timeout.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Adapter is a pure-Go IRC channel adapter implementing channel.ChannelAdapter.
type Adapter struct {
	server   string   // host:port of the IRC server
	nick     string   // bot nickname (also used for USER/realname)
	channels []string // channels to JOIN on connect (e.g. "#ops")
	tenant   string   // tenant attached to every inbound message
	agent    string   // target agent attached to every inbound message

	dial    dialFunc
	handler channel.InboundHandler

	mu     sync.Mutex
	conn   net.Conn
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

// New constructs an IRC adapter. server is "host:port"; channels is the list of
// channels to join (names may be given with or without a leading '#').
func New(server, nick string, channels []string, tenant, agent string) *Adapter {
	norm := make([]string, 0, len(channels))
	for _, c := range channels {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.HasPrefix(c, "#") && !strings.HasPrefix(c, "&") {
			c = "#" + c
		}
		norm = append(norm, c)
	}
	if nick == "" {
		nick = "cyntr"
	}
	return &Adapter{
		server:   server,
		nick:     nick,
		channels: norm,
		tenant:   tenant,
		agent:    agent,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 30 * time.Second}
			return d.DialContext(ctx, network, addr)
		},
	}
}

// SetDialFunc overrides the dialer (used by tests).
func (a *Adapter) SetDialFunc(f dialFunc) { a.dial = f }

func (a *Adapter) Name() string { return "irc" }

// Start dials the IRC server, registers, joins channels, and begins reading
// inbound lines in a background goroutine. It honors ctx: when ctx is
// cancelled (or Stop is called) the read loop exits and the connection closes.
func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler

	conn, err := a.dial(ctx, "tcp", a.server)
	if err != nil {
		return fmt.Errorf("irc dial %q: %w", a.server, err)
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		conn.Close()
		return fmt.Errorf("irc: adapter already stopped")
	}
	a.conn = conn
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.mu.Unlock()

	// Register with the server, then join channels.
	if err := a.writeLine("NICK " + a.nick); err != nil {
		conn.Close()
		return fmt.Errorf("irc register nick: %w", err)
	}
	if err := a.writeLine(fmt.Sprintf("USER %s 0 * :%s", a.nick, a.nick)); err != nil {
		conn.Close()
		return fmt.Errorf("irc register user: %w", err)
	}
	for _, ch := range a.channels {
		if err := a.writeLine("JOIN " + ch); err != nil {
			conn.Close()
			return fmt.Errorf("irc join %q: %w", ch, err)
		}
	}

	a.wg.Add(1)
	go a.readLoop(runCtx, conn)

	return nil
}

// Stop cancels the read loop and closes the connection. Safe to call multiple
// times. Blocks until the read goroutine has exited (no goroutine leak),
// bounded by ctx.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	if a.cancel != nil {
		a.cancel()
	}
	if a.conn != nil {
		a.conn.Close()
	}
	a.mu.Unlock()

	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send formats and writes an outbound PRIVMSG to the target channel/user.
// ChannelID is the IRC target (channel name like "#ops" or a nick).
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if msg.ChannelID == "" {
		return fmt.Errorf("irc: missing channel ID (target)")
	}
	a.mu.Lock()
	conn := a.conn
	closed := a.closed
	a.mu.Unlock()
	if conn == nil || closed {
		return fmt.Errorf("irc: not connected")
	}
	// IRC is line-oriented; split multi-line text into one PRIVMSG per line so
	// embedded newlines never break the protocol framing.
	for _, line := range strings.Split(msg.Text, "\n") {
		if line == "" {
			continue
		}
		if err := a.writeLine(fmt.Sprintf("PRIVMSG %s :%s", msg.ChannelID, line)); err != nil {
			return fmt.Errorf("irc send: %w", err)
		}
	}
	return nil
}

// writeLine writes a single CRLF-terminated IRC protocol line under the lock.
func (a *Adapter) writeLine(line string) error {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("irc: no connection")
	}
	_, err := conn.Write([]byte(line + "\r\n"))
	return err
}

// readLoop consumes inbound IRC lines until ctx is cancelled or the connection
// closes. It answers PING with PONG and dispatches PRIVMSG to the handler.
func (a *Adapter) readLoop(ctx context.Context, conn net.Conn) {
	defer a.wg.Done()
	reader := bufio.NewReader(conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			select {
			case <-ctx.Done():
				// expected on Stop
			default:
				logger.Warn("irc read loop ended", map[string]any{"error": err.Error()})
			}
			return
		}
		a.handleLine(strings.TrimRight(line, "\r\n"))
	}
}

// handleLine parses one IRC protocol line.
func (a *Adapter) handleLine(line string) {
	if line == "" {
		return
	}

	// PING keepalive: "PING :token" => "PONG :token".
	if strings.HasPrefix(line, "PING") {
		token := ""
		if i := strings.Index(line, ":"); i >= 0 {
			token = line[i+1:]
		} else if fields := strings.Fields(line); len(fields) > 1 {
			token = fields[1]
		}
		_ = a.writeLine("PONG :" + token)
		return
	}

	prefix, command, params := parseMessage(line)
	if command != "PRIVMSG" || len(params) < 2 {
		return
	}

	target := params[0]
	text := params[1]
	sender := nickFromPrefix(prefix)

	// Ignore our own echoed messages if any server reflects them.
	if sender == a.nick {
		return
	}

	// ChannelID for replies: if addressed to a channel, reply to the channel;
	// if a direct query (target is our nick), reply to the sender.
	channelID := target
	if !strings.HasPrefix(target, "#") && !strings.HasPrefix(target, "&") {
		channelID = sender
	}

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel:   "irc",
			ChannelID: channelID,
			UserID:    sender,
			Text:      text,
			Tenant:    a.tenant,
			Agent:     a.agent,
		})
		if err != nil {
			logger.Error("message handler failed", map[string]any{"error": err.Error()})
			return
		}
		if response == "" {
			return
		}
		if err := a.Send(context.Background(), channel.OutboundMessage{
			Channel:   "irc",
			ChannelID: channelID,
			Text:      response,
		}); err != nil {
			logger.Warn("irc reply failed", map[string]any{"error": err.Error()})
		}
	}()
}

// parseMessage parses an IRC line into (prefix, command, params). The trailing
// parameter (introduced by " :") may contain spaces and is returned as the
// final element of params.
func parseMessage(line string) (prefix, command string, params []string) {
	if strings.HasPrefix(line, ":") {
		if sp := strings.IndexByte(line, ' '); sp >= 0 {
			prefix = line[1:sp]
			line = strings.TrimLeft(line[sp+1:], " ")
		} else {
			return line[1:], "", nil
		}
	}

	var trailing string
	hasTrailing := false
	if i := strings.Index(line, " :"); i >= 0 {
		trailing = line[i+2:]
		hasTrailing = true
		line = line[:i]
	}

	fields := strings.Fields(line)
	if len(fields) == 0 {
		if hasTrailing {
			return prefix, "", []string{trailing}
		}
		return prefix, "", nil
	}
	command = fields[0]
	params = append(params, fields[1:]...)
	if hasTrailing {
		params = append(params, trailing)
	}
	return prefix, command, params
}

// nickFromPrefix extracts the nickname from an IRC prefix "nick!user@host".
func nickFromPrefix(prefix string) string {
	if i := strings.IndexByte(prefix, '!'); i >= 0 {
		return prefix[:i]
	}
	return prefix
}

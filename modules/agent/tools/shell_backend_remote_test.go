package tools

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// ---- Singularity ----

func TestSingularityArgs(t *testing.T) {
	got := singularityArgs("img.sif", "echo hi")
	want := []string{"exec", "img.sif", "bash", "-c", "echo hi"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestSingularityRunUsesRunner(t *testing.T) {
	var gotBin string
	var gotArgs []string
	b := NewSingularityBackend("img.sif", "")
	b.runner = func(_ context.Context, name string, args ...string) (string, error) {
		gotBin, gotArgs = name, args
		return "OUTPUT", nil
	}
	out, err := b.Run(context.Background(), "acme", "ls -la", 0)
	if err != nil || out != "OUTPUT" {
		t.Fatalf("run: out=%q err=%v", out, err)
	}
	if gotBin != "singularity" {
		t.Fatalf("binary = %q, want singularity", gotBin)
	}
	if strings.Join(gotArgs, "|") != "exec|img.sif|bash|-c|ls -la" {
		t.Fatalf("args = %v", gotArgs)
	}
}

func TestSingularityMissingBinary(t *testing.T) {
	b := NewSingularityBackend("img.sif", "definitely-not-installed-xyz")
	if _, err := b.Run(context.Background(), "acme", "echo hi", 0); err == nil {
		t.Fatal("missing binary should error cleanly")
	}
}

func TestSingularityNoImage(t *testing.T) {
	b := NewSingularityBackend("", "")
	if _, err := b.Run(context.Background(), "acme", "echo hi", 0); err == nil {
		t.Fatal("no image should error")
	}
}

// ---- SSH ----

// startTestSSHServer runs an in-process SSH server that, for any exec request,
// replies "echoed: <command>" and exits 0. Returns its address and host key.
func startTestSSHServer(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(c, cfg)
		}
	}()
	return ln.Addr().String(), hostSigner.PublicKey()
}

func serveSSHConn(c net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			for req := range requests {
				if req.Type == "exec" && len(req.Payload) >= 4 {
					command := string(req.Payload[4:])
					req.Reply(true, nil)
					ch.Write([]byte("echoed: " + command + "\n"))
					ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					ch.Close()
				} else {
					req.Reply(false, nil)
				}
			}
		}()
	}
}

func TestSSHBackendRoundTrip(t *testing.T) {
	addr, hostKey := startTestSSHServer(t)
	b := &SSHBackend{
		Addr:            addr,
		User:            "tester",
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     5 * time.Second,
	}
	out, err := b.Run(context.Background(), "acme", "whoami", 5*time.Second)
	if err != nil {
		t.Fatalf("ssh run: %v", err)
	}
	if !strings.Contains(out, "echoed: whoami") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestSSHBackendDialError(t *testing.T) {
	b := &SSHBackend{Addr: "127.0.0.1:1", User: "x", HostKeyCallback: ssh.InsecureIgnoreHostKey(), DialTimeout: time.Second}
	if _, err := b.Run(context.Background(), "acme", "whoami", time.Second); err == nil {
		t.Fatal("dial to a dead port should error cleanly")
	}
}

func TestNewSSHBackendFromKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(block)
	b, err := NewSSHBackendFromKey("host:22", "user", keyPEM, nil)
	if err != nil {
		t.Fatalf("NewSSHBackendFromKey: %v", err)
	}
	if b.User != "user" || b.Addr != "host:22" || len(b.Auth) != 1 {
		t.Fatalf("backend not configured: %+v", b)
	}
}

package tools

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHBackend runs shell commands on a remote host over SSH. It satisfies the
// same ShellBackend interface as the in-process and Docker backends, so the
// tool API is unchanged — only where the command runs differs.
type SSHBackend struct {
	Addr            string // host:port
	User            string
	Auth            []ssh.AuthMethod
	HostKeyCallback ssh.HostKeyCallback
	DialTimeout     time.Duration
}

// NewSSHBackendFromKey builds an SSHBackend authenticating with a PEM private
// key. hostKey, when non-nil, pins the server's host key; pass nil only in
// trusted/test settings (it disables host-key verification).
func NewSSHBackendFromKey(addr, user string, privateKeyPEM []byte, hostKey ssh.PublicKey) (*SSHBackend, error) {
	signer, err := ssh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("ssh backend: parse key: %w", err)
	}
	hkc := ssh.InsecureIgnoreHostKey()
	if hostKey != nil {
		hkc = ssh.FixedHostKey(hostKey)
	}
	return &SSHBackend{
		Addr:            addr,
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hkc,
		DialTimeout:     10 * time.Second,
	}, nil
}

// Run executes command on the remote host and returns merged stdout+stderr.
func (b *SSHBackend) Run(ctx context.Context, _ string, command string, timeout time.Duration) (string, error) {
	if b == nil || b.Addr == "" {
		return "", fmt.Errorf("ssh backend not configured")
	}
	hkc := b.HostKeyCallback
	if hkc == nil {
		hkc = ssh.InsecureIgnoreHostKey()
	}
	dialTimeout := b.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 10 * time.Second
	}

	client, err := ssh.Dial("tcp", b.Addr, &ssh.ClientConfig{
		User:            b.User,
		Auth:            b.Auth,
		HostKeyCallback: hkc,
		Timeout:         dialTimeout,
	})
	if err != nil {
		return "", fmt.Errorf("ssh backend: dial %s: %w", b.Addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh backend: new session: %w", err)
	}
	defer session.Close()

	if timeout <= 0 {
		timeout = shellDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := session.CombinedOutput(command)
		done <- result{out, err}
	}()

	select {
	case <-runCtx.Done():
		session.Signal(ssh.SIGKILL)
		return "", fmt.Errorf("ssh backend: command timed out: %w", runCtx.Err())
	case r := <-done:
		output := truncateOutput(string(r.out))
		if r.err != nil {
			return output, fmt.Errorf("command failed: %w", r.err)
		}
		return output, nil
	}
}

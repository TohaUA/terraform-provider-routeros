package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func generateTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("failed to create ssh signer: %v", err)
	}
	return signer
}

func startMockSSHServer(t *testing.T, hostKey ssh.Signer) (addr string, authCalled *atomic.Bool) {
	t.Helper()
	called := &atomic.Bool{}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			called.Store(true)
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer sconn.Close()
				go ssh.DiscardRequests(reqs)
				for newChan := range chans {
					_ = newChan.Reject(ssh.Prohibited, "not implemented")
				}
			}(c)
		}
	}()

	return ln.Addr().String(), called
}

func TestNormalizeSSHAddr(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"192.168.88.1", "192.168.88.1:22"},
		{"192.168.88.1:2222", "192.168.88.1:2222"},
		{"::1", "[::1]:22"},
		{"[::1]", "[::1]:22"},
		{"[::1]:2222", "[::1]:2222"},
		{"router.example.com", "router.example.com:22"},
	}

	for _, tc := range tests {
		if got := normalizeSSHAddr(tc.in); got != tc.want {
			t.Errorf("normalizeSSHAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildHostKeyCallbackValidation(t *testing.T) {
	if _, err := BuildHostKeyCallback(HostKeyOptions{
		InsecureIgnoreHostKey: true,
		HostKeyFingerprint:    "SHA256:abc",
	}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error when both -insecure-ignore-host-key and -host-key-fingerprint are set")
	}

	for _, bad := range []string{"MD5:aa:bb", "SHA256:", "SHA256:===", "invalid"} {
		if _, err := BuildHostKeyCallback(HostKeyOptions{
			HostKeyFingerprint: bad,
		}, &bytes.Buffer{}); err == nil {
			t.Errorf("expected error for malformed fingerprint %q", bad)
		}
	}

	if _, err := BuildHostKeyCallback(HostKeyOptions{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error when known_hosts path is empty")
	}

	missingPath := filepath.Join(t.TempDir(), "does_not_exist")
	_, err := BuildHostKeyCallback(HostKeyOptions{KnownHostsPath: missingPath}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when known_hosts file does not exist")
	}
	for _, flagName := range []string{"-known-hosts", "-host-key-fingerprint", "-insecure-ignore-host-key"} {
		if !strings.Contains(err.Error(), flagName) {
			t.Errorf("expected error %q to reference %s", err.Error(), flagName)
		}
	}
}

func TestNewSshRejectsNilCallback(t *testing.T) {
	if _, err := NewSsh("127.0.0.1:22", "admin", "secret", nil); err == nil {
		t.Fatal("expected NewSsh with nil HostKeyCallback to fail")
	}
}

func TestSSHHostKeyKnownHostsVerification(t *testing.T) {
	serverSigner := generateTestSigner(t)
	wrongSigner := generateTestSigner(t)
	addr, authCalled := startMockSSHServer(t, serverSigner)

	t.Run("unknown host rejected before auth", func(t *testing.T) {
		authCalled.Store(false)
		khPath := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(khPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		cb, err := BuildHostKeyCallback(HostKeyOptions{KnownHostsPath: khPath}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error building callback: %v", err)
		}
		if _, err := NewSsh(addr, "admin", "secret", cb); err == nil {
			t.Fatal("expected dial to fail for unknown host")
		}
		if authCalled.Load() {
			t.Fatal("password auth must not be attempted when host key is unknown")
		}
	})

	t.Run("mismatched key rejected before auth", func(t *testing.T) {
		authCalled.Store(false)
		khPath := filepath.Join(t.TempDir(), "known_hosts")
		line := knownhosts.Line([]string{addr}, wrongSigner.PublicKey()) + "\n"
		if err := os.WriteFile(khPath, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}

		cb, err := BuildHostKeyCallback(HostKeyOptions{KnownHostsPath: khPath}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error building callback: %v", err)
		}
		if _, err := NewSsh(addr, "admin", "secret", cb); err == nil {
			t.Fatal("expected dial to fail on host key mismatch")
		}
		if authCalled.Load() {
			t.Fatal("password auth must not be attempted when host key mismatches")
		}
	})

	t.Run("matching known_hosts succeeds", func(t *testing.T) {
		authCalled.Store(false)
		khPath := filepath.Join(t.TempDir(), "known_hosts")
		line := knownhosts.Line([]string{addr}, serverSigner.PublicKey()) + "\n"
		if err := os.WriteFile(khPath, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}

		cb, err := BuildHostKeyCallback(HostKeyOptions{KnownHostsPath: khPath}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error building callback: %v", err)
		}
		conn, err := NewSsh(addr, "admin", "secret", cb)
		if err != nil {
			t.Fatalf("expected dial to succeed: %v", err)
		}
		_ = conn.Close()
		if !authCalled.Load() {
			t.Fatal("expected auth to be called after host key verification succeeded")
		}
	})
}

func TestSSHHostKeyFingerprintVerification(t *testing.T) {
	serverSigner := generateTestSigner(t)
	wrongSigner := generateTestSigner(t)
	addr, authCalled := startMockSSHServer(t, serverSigner)
	missingKH := filepath.Join(t.TempDir(), "nonexistent_known_hosts")

	t.Run("matching fingerprint succeeds even without known_hosts", func(t *testing.T) {
		authCalled.Store(false)
		fp := ssh.FingerprintSHA256(serverSigner.PublicKey())
		cb, err := BuildHostKeyCallback(HostKeyOptions{
			KnownHostsPath:     missingKH,
			HostKeyFingerprint: fp,
		}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		conn, err := NewSsh(addr, "admin", "secret", cb)
		if err != nil {
			t.Fatalf("expected dial to succeed: %v", err)
		}
		_ = conn.Close()
		if !authCalled.Load() {
			t.Fatal("expected auth to be called")
		}
	})

	t.Run("matching fingerprint with trailing padding succeeds", func(t *testing.T) {
		authCalled.Store(false)
		fp := ssh.FingerprintSHA256(serverSigner.PublicKey()) + "="
		cb, err := BuildHostKeyCallback(HostKeyOptions{
			KnownHostsPath:     missingKH,
			HostKeyFingerprint: fp,
		}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		conn, err := NewSsh(addr, "admin", "secret", cb)
		if err != nil {
			t.Fatalf("expected dial to succeed with padded fingerprint: %v", err)
		}
		_ = conn.Close()
	})

	t.Run("mismatched fingerprint rejected before auth", func(t *testing.T) {
		authCalled.Store(false)
		wrongFP := ssh.FingerprintSHA256(wrongSigner.PublicKey())
		cb, err := BuildHostKeyCallback(HostKeyOptions{
			KnownHostsPath:     missingKH,
			HostKeyFingerprint: wrongFP,
		}, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := NewSsh(addr, "admin", "secret", cb); err == nil {
			t.Fatal("expected dial to fail on fingerprint mismatch")
		}
		if authCalled.Load() {
			t.Fatal("password auth must not be attempted when fingerprint mismatches")
		}
	})
}

func TestSSHInsecureIgnoreHostKeyEmitsWarning(t *testing.T) {
	serverSigner := generateTestSigner(t)
	addr, authCalled := startMockSSHServer(t, serverSigner)

	var stderr bytes.Buffer
	cb, err := BuildHostKeyCallback(HostKeyOptions{
		InsecureIgnoreHostKey: true,
	}, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "WARNING") || !strings.Contains(stderr.String(), "-insecure-ignore-host-key") {
		t.Fatalf("expected warning on stderr, got %q", stderr.String())
	}

	conn, err := NewSsh(addr, "admin", "secret", cb)
	if err != nil {
		t.Fatalf("expected dial to succeed with -insecure-ignore-host-key: %v", err)
	}
	_ = conn.Close()
	if !authCalled.Load() {
		t.Fatal("expected auth to be called")
	}
}

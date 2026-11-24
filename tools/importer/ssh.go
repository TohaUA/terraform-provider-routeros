package main

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type HostKeyOptions struct {
	KnownHostsPath        string
	HostKeyFingerprint    string
	InsecureIgnoreHostKey bool
}

func defaultKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func BuildHostKeyCallback(opts HostKeyOptions, stderr io.Writer) (ssh.HostKeyCallback, error) {
	fingerprint := strings.TrimSpace(opts.HostKeyFingerprint)

	if opts.InsecureIgnoreHostKey && fingerprint != "" {
		return nil, fmt.Errorf("-insecure-ignore-host-key and -host-key-fingerprint are mutually exclusive")
	}

	if opts.InsecureIgnoreHostKey {
		if stderr != nil {
			fmt.Fprintln(stderr, "WARNING: SSH host key verification disabled (-insecure-ignore-host-key); connection is vulnerable to MITM attacks.")
		}
		return ssh.InsecureIgnoreHostKey(), nil
	}

	if fingerprint != "" {
		if !strings.HasPrefix(fingerprint, "SHA256:") {
			return nil, fmt.Errorf("invalid -host-key-fingerprint %q: must start with \"SHA256:\"", opts.HostKeyFingerprint)
		}
		normalized := "SHA256:" + strings.TrimRight(strings.TrimPrefix(fingerprint, "SHA256:"), "=")
		if len(normalized) <= len("SHA256:") {
			return nil, fmt.Errorf("invalid -host-key-fingerprint %q: empty SHA256 digest", opts.HostKeyFingerprint)
		}
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			actual := ssh.FingerprintSHA256(key)
			if subtle.ConstantTimeCompare([]byte(actual), []byte(normalized)) != 1 {
				return fmt.Errorf("SSH host key fingerprint mismatch for %s (%s): got %s, want %s", hostname, remote, actual, normalized)
			}
			return nil
		}, nil
	}

	knownHostsPath := strings.TrimSpace(opts.KnownHostsPath)
	if knownHostsPath == "" {
		return nil, fmt.Errorf("no known_hosts path available: specify -known-hosts, -host-key-fingerprint, or -insecure-ignore-host-key")
	}

	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load known_hosts file %q: %w (provide a valid -known-hosts file, pin -host-key-fingerprint, or pass -insecure-ignore-host-key)", knownHostsPath, err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := cb(hostname, remote, key); err != nil {
			return fmt.Errorf("SSH host key verification failed for %s using -known-hosts %q (add the host key, pin -host-key-fingerprint, or pass -insecure-ignore-host-key): %w", hostname, knownHostsPath, err)
		}
		return nil
	}, nil
}

func normalizeSSHAddr(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	trimmed := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.JoinHostPort(trimmed, "22")
}

type SshConnection struct {
	client *ssh.Client
}

func NewSsh(host, username, password string, hostKeyCallback ssh.HostKeyCallback) (*SshConnection, error) {
	if hostKeyCallback == nil {
		return nil, fmt.Errorf("SSH host key callback must not be nil")
	}

	addr := normalizeSSHAddr(host)
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	return &SshConnection{
		client: client,
	}, nil
}

func (c *SshConnection) Close() error {
	return c.client.Close()
}

func (c *SshConnection) Run(cmd string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session, %v", err)
	}
	defer session.Close()

	var b bytes.Buffer
	session.Stdout = &b
	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("failed to run, %v", err.Error())
	}
	return b.String(), nil
}

func GetMikrotikConfig(conn *SshConnection) (string, error) {
	return conn.Run("/export terse")
}

func GetResourceId(conn *SshConnection, path string, requiredFields []string) string {
	var id string
	var filter = strings.Join(requiredFields, " ")

	cmd := fmt.Sprintf(":put [%v get [ find %v ]]", path, filter)
	log.Debug("Searching id in ", path, " with command: ", cmd)
	res, err := conn.Run(cmd)
	if err != nil {
		log.Error("Error running command: ", err)
		return "?"
	}

	ss := reId.FindStringSubmatch(res)
	log.Debug("ss is ", ss)
	if len(ss) == 2 {
		id = ss[1]
		log.Info("Found id ", id, " for ", filter)
		return id
	}

	// Let's try with dynamic=no
	cmd = fmt.Sprintf(":put [%v get [ find %v dynamic=no ]]", path, filter)
	log.Debug("Searching id in ", path, " with command: ", cmd)
	res, err = conn.Run(cmd)
	if err != nil {
		log.Error("Error running command: ", err)
		return "?"
	}

	ss = reId.FindStringSubmatch(res)
	log.Debug("ss is ", ss)
	if len(ss) == 2 {
		id = ss[1]
		log.Info("Found id ", id, " for ", filter)
		return id
	}

	if id == "" {
		log.Warnf("Id not found for %v (filter: %v)", requiredFields, filter)
		return "?"
	}

	return id
}

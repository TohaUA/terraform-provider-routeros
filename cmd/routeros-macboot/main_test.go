// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/terraform-routeros/terraform-provider-routeros/internal/macboot"
)

func TestCLIBootstrapAndShellRefuseWithoutAllowInsecureL2(t *testing.T) {
	called := false
	deps := cliDeps{
		bootstrap: func(ctx context.Context, cfg macboot.BootstrapConfig) (macboot.BootstrapResult, error) {
			called = true
			return macboot.BootstrapResult{}, nil
		},
		shell: func(ctx context.Context, opts macboot.MACTelnetOptions, stdin io.Reader, stdout io.Writer) error {
			called = true
			return nil
		},
	}

	for _, args := range [][]string{
		{"bootstrap", "--interface", "eth0", "--mac", "54:05:AB:01:02:03", "--address", "172.30.99.2/24", "--target-interface", "ether2"},
		{"shell", "--interface", "eth0", "--mac", "54:05:AB:01:02:03"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &stdout, &stderr, deps)
		if code == 0 {
			t.Fatalf("args %v: expected non-zero exit code", args)
		}
		if !strings.Contains(stderr.String(), "allow_insecure_l2") {
			t.Fatalf("args %v: expected allow_insecure_l2 error in stderr, got %q", args, stderr.String())
		}
		if called {
			t.Fatalf("args %v: dependency should not be invoked without --allow-insecure-l2", args)
		}
	}
}

func TestCLIDiscoverJSONAndTable(t *testing.T) {
	deps := cliDeps{
		discover: func(ctx context.Context, opts macboot.DiscoverOptions) ([]macboot.MNDPNeighbor, error) {
			if opts.Interface != "macbootbr0" {
				t.Fatalf("unexpected interface %q", opts.Interface)
			}
			return []macboot.MNDPNeighbor{
				{
					MACAddress:    "54:05:AB:01:02:03",
					Identity:      "MikroTik",
					Version:       "7.25.2",
					Board:         "CHR",
					InterfaceName: "ether2",
				},
			}, nil
		},
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"discover", "--interface", "macbootbr0", "--json"}, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("discover --json exit %d: %s", code, stderr.String())
	}
	var got []macboot.MNDPNeighbor
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout.String())
	}
	if len(got) != 1 || got[0].MACAddress != "54:05:AB:01:02:03" {
		t.Fatalf("unexpected JSON neighbors: %+v", got)
	}
}

func TestCLIBootstrapWarningsAndFingerprintMismatch(t *testing.T) {
	fp := strings.Repeat("AB", 32)
	deps := cliDeps{
		bootstrap: func(ctx context.Context, cfg macboot.BootstrapConfig) (macboot.BootstrapResult, error) {
			if cfg.ExpectedTLSFingerprint != "" && cfg.ExpectedTLSFingerprint != fp {
				return macboot.BootstrapResult{}, errors.New("bootstrap TLS fingerprint mismatch")
			}
			return macboot.BootstrapResult{
				TLSFingerprint:  fp,
				Address:         cfg.Address,
				RouterAddress:   "172.30.99.2",
				Identity:        "MikroTik",
				RouterOSVersion: "7.25.2 (stable)",
			}, nil
		},
	}

	// Without --expected-tls-fingerprint: succeeds, warns on stderr about insecure L2 + password rotation.
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"bootstrap",
		"--interface", "macbootbr0",
		"--mac", "54:05:AB:01:02:03",
		"--address", "172.30.99.2/24",
		"--target-interface", "ether2",
		"--allow-insecure-l2",
		"--json",
	}, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("bootstrap exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cleartext") || !strings.Contains(stderr.String(), "rotate the admin password") {
		t.Fatalf("expected insecure L2 + password rotation notices on stderr, got: %q", stderr.String())
	}

	// Mismatched --expected-tls-fingerprint: fails closed with exit 1.
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"bootstrap",
		"--interface", "macbootbr0",
		"--mac", "54:05:AB:01:02:03",
		"--address", "172.30.99.2/24",
		"--target-interface", "ether2",
		"--allow-insecure-l2",
		"--expected-tls-fingerprint", strings.Repeat("00", 32),
	}, strings.NewReader(""), &stdout, &stderr, deps)
	if code == 0 {
		t.Fatalf("expected non-zero exit code on fingerprint mismatch")
	}
}

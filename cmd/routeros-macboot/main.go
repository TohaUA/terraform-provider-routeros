// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/terraform-routeros/terraform-provider-routeros/internal/macboot"
	"golang.org/x/term"
)

const insecureL2Warning = "WARNING: layer-2 MAC-Telnet authenticates with EC-SRP5, but all post-login commands and output are sent in cleartext without server authentication. Use only on trusted, isolated segments."

type cliDeps struct {
	discover  func(ctx context.Context, opts macboot.DiscoverOptions) ([]macboot.MNDPNeighbor, error)
	bootstrap func(ctx context.Context, cfg macboot.BootstrapConfig) (macboot.BootstrapResult, error)
	shell     func(ctx context.Context, opts macboot.MACTelnetOptions, stdin io.Reader, stdout io.Writer) error
}

func defaultDeps() cliDeps {
	return cliDeps{
		discover:  macboot.DiscoverMNDP,
		bootstrap: macboot.Bootstrap,
		shell: func(ctx context.Context, opts macboot.MACTelnetOptions, stdin io.Reader, stdout io.Writer) error {
			sess, err := macboot.DialMACTelnet(ctx, opts)
			if err != nil {
				return err
			}
			defer sess.Close()

			if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
				oldState, rawErr := term.MakeRaw(int(f.Fd()))
				if rawErr == nil {
					defer func() { _ = term.Restore(int(f.Fd()), oldState) }()
				}
			}
			return sess.Shell(ctx, stdin, stdout)
		},
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, defaultDeps()))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps cliDeps) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	subcmd := args[0]
	rest := args[1:]
	switch subcmd {
	case "discover":
		return runDiscover(ctx, rest, stdout, stderr, deps)
	case "bootstrap":
		return runBootstrap(ctx, rest, stdout, stderr, deps)
	case "shell":
		return runShell(ctx, rest, stdin, stdout, stderr, deps)
	case "-h", "--help", "help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n", subcmd)
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: routeros-macboot <discover|bootstrap|shell> [flags]")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  discover   Send MNDP layer-2 discovery probes on a local interface")
	fmt.Fprintln(w, "  bootstrap  Assign IPv4 + TLS certificate over MAC-Telnet and verify HTTPS REST")
	fmt.Fprintln(w, "  shell      Open an interactive debug MAC-Telnet shell")
}

func runDiscover(ctx context.Context, args []string, stdout, stderr io.Writer, deps cliDeps) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		iface   string
		timeout time.Duration
		asJSON  bool
	)
	fs.StringVar(&iface, "interface", "", "Local network interface name")
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "Discovery timeout")
	fs.BoolVar(&asJSON, "json", false, "Emit JSON output")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if iface == "" {
		fmt.Fprintln(stderr, "error: --interface is required")
		return 2
	}

	neighbors, err := deps.discover(ctx, macboot.DiscoverOptions{
		Interface: iface,
		Timeout:   timeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if asJSON {
		if neighbors == nil {
			neighbors = []macboot.MNDPNeighbor{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(neighbors); err != nil {
			fmt.Fprintf(stderr, "error encoding JSON: %v\n", err)
			return 1
		}
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MAC\tIDENTITY\tVERSION\tBOARD\tINTERFACE\tIPV4\tIPV6")
	for _, n := range neighbors {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			n.MACAddress, n.Identity, n.Version, n.Board, n.InterfaceName, n.IPv4Address, n.IPv6Address)
	}
	_ = tw.Flush()
	return 0
}

func runBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer, deps cliDeps) int {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		iface          string
		timeout        time.Duration
		mac            string
		address        string
		targetIface    string
		username       string
		password       string
		allowInsecure  bool
		expectedFP     string
		certName       string
		enableAPISSL   bool
		unsafeRawDebug bool
		asJSON         bool
	)
	fs.StringVar(&iface, "interface", "", "Local network interface name")
	fs.DurationVar(&timeout, "timeout", 30*time.Second, "Bootstrap timeout")
	fs.StringVar(&mac, "mac", "", "Target RouterOS MAC address")
	fs.StringVar(&address, "address", "", "IPv4 CIDR to assign on target interface (for example 192.168.88.1/24)")
	fs.StringVar(&targetIface, "target-interface", "", "Target interface name on RouterOS (for example ether2)")
	fs.StringVar(&username, "username", "admin", "RouterOS username")
	fs.StringVar(&password, "password", "", "RouterOS password")
	fs.BoolVar(&allowInsecure, "allow-insecure-l2", false, "Explicit opt-in to cleartext layer-2 MAC-Telnet session")
	fs.StringVar(&expectedFP, "expected-tls-fingerprint", "", "Expected SHA-256 TLS certificate fingerprint")
	fs.StringVar(&certName, "certificate-name", macboot.DefaultCertificateName, "RouterOS certificate name")
	fs.BoolVar(&enableAPISSL, "enable-api-ssl", false, "Also enable api-ssl with the bootstrap certificate")
	fs.BoolVar(&unsafeRawDebug, "unsafe-raw-debug", false, "Emit raw MAC-Telnet hex packet dumps to stderr")
	fs.BoolVar(&asJSON, "json", false, "Emit JSON output")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !allowInsecure {
		fmt.Fprintf(stderr, "error: %v (pass --allow-insecure-l2 to acknowledge cleartext L2 transport)\n", macboot.ErrInsecureL2Required)
		return 2
	}
	if iface == "" || mac == "" || address == "" || targetIface == "" {
		fmt.Fprintln(stderr, "error: --interface, --mac, --address, and --target-interface are required")
		return 2
	}

	fmt.Fprintln(stderr, insecureL2Warning)
	if expectedFP == "" {
		fmt.Fprintln(stderr, "NOTICE: --expected-tls-fingerprint was not supplied; rotate the admin password over TLS and pin the returned tls_fingerprint afterwards.")
	}

	cfg := macboot.BootstrapConfig{
		Interface:              iface,
		MACAddress:             mac,
		Address:                address,
		TargetInterface:        targetIface,
		Username:               username,
		Password:               password,
		AllowInsecureL2:        allowInsecure,
		ExpectedTLSFingerprint: expectedFP,
		CertificateName:        certName,
		EnableAPISSL:           enableAPISSL,
		Timeout:                timeout,
		UnsafeRawDebug:         unsafeRawDebug,
		Logger: func(format string, a ...any) {
			fmt.Fprintf(stderr, format+"\n", a...)
		},
	}

	res, err := deps.bootstrap(ctx, cfg)
	if err != nil {
		if errors.Is(err, macboot.ErrInsecureL2Required) {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "error encoding JSON: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "tls_fingerprint=%s\n", res.TLSFingerprint)
	fmt.Fprintf(stdout, "router_address=%s\n", res.RouterAddress)
	fmt.Fprintf(stdout, "address=%s\n", res.Address)
	fmt.Fprintf(stdout, "identity=%s\n", res.Identity)
	fmt.Fprintf(stdout, "routeros_version=%s\n", res.RouterOSVersion)
	return 0
}

func runShell(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps cliDeps) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		iface          string
		timeout        time.Duration
		mac            string
		username       string
		password       string
		allowInsecure  bool
		unsafeRawDebug bool
	)
	fs.StringVar(&iface, "interface", "", "Local network interface name")
	fs.DurationVar(&timeout, "timeout", 30*time.Second, "Connection timeout")
	fs.StringVar(&mac, "mac", "", "Target RouterOS MAC address")
	fs.StringVar(&username, "username", "admin", "RouterOS username")
	fs.StringVar(&password, "password", "", "RouterOS password")
	fs.BoolVar(&allowInsecure, "allow-insecure-l2", false, "Explicit opt-in to cleartext layer-2 MAC-Telnet session")
	fs.BoolVar(&unsafeRawDebug, "unsafe-raw-debug", false, "Emit raw MAC-Telnet hex packet dumps to stderr")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !allowInsecure {
		fmt.Fprintf(stderr, "error: %v (pass --allow-insecure-l2 to acknowledge cleartext L2 transport)\n", macboot.ErrInsecureL2Required)
		return 2
	}
	if iface == "" || mac == "" {
		fmt.Fprintln(stderr, "error: --interface and --mac are required")
		return 2
	}

	fmt.Fprintln(stderr, insecureL2Warning)

	err := deps.shell(ctx, macboot.MACTelnetOptions{
		Interface:      iface,
		TargetMAC:      mac,
		Username:       username,
		Password:       password,
		Timeout:        timeout,
		UnsafeRawDebug: unsafeRawDebug,
		Logger: func(format string, a ...any) {
			fmt.Fprintf(stderr, format+"\n", a...)
		},
	}, stdin, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

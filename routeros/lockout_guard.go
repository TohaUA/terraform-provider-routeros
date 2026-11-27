package routeros

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const KeyLockoutAck = "lockout_ack"

var PropLockoutAck = &schema.Schema{
	Type:        schema.TypeString,
	Optional:    true,
	Description: "Per-change acknowledgement token required when intentionally applying a management lockout-risky change. Set this to the exact token reported by the lockout guard diagnostic.",
}

func checkIpServiceLockout(_ context.Context, service string, disabled bool, port int, address string, ack string, c Client) error {
	if c == nil {
		return nil
	}

	extra := c.GetExtraParams()
	if extra == nil || extra.ActiveService == "" || service != extra.ActiveService {
		return nil
	}

	var risks []string
	if disabled {
		risks = append(risks, "disable")
	}
	if extra.ActivePort > 0 && port > 0 && port != extra.ActivePort {
		risks = append(risks, fmt.Sprintf("port=%d", port))
	}
	trimmedAddr := strings.TrimSpace(address)
	if trimmedAddr != "" && ipServiceAddressExcludesConnection(trimmedAddr, extra) {
		risks = append(risks, fmt.Sprintf("address=%s", trimmedAddr))
	}

	if len(risks) == 0 {
		return nil
	}

	expectedToken := "ip-service:" + service + ":" + strings.Join(risks, ",")
	if strings.TrimSpace(ack) == expectedToken {
		return nil
	}

	return fmt.Errorf(
		"refusing to modify active management service %q (%s) because it would lock out the active Terraform connection; if intentional, set lockout_ack = %q",
		extra.ActiveService, strings.Join(risks, ", "), expectedToken,
	)
}

func parseIpServiceEntry(entry string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(entry); err == nil {
		return p.Masked(), true
	}
	if a, err := netip.ParseAddr(entry); err == nil {
		u := a.Unmap()
		return netip.PrefixFrom(u, u.BitLen()), true
	}
	if ipStr, maskStr, ok := strings.Cut(entry, "/"); ok {
		if ip, err := netip.ParseAddr(strings.TrimSpace(ipStr)); err == nil && ip.Is4() {
			if maskAddr, err := netip.ParseAddr(strings.TrimSpace(maskStr)); err == nil && maskAddr.Is4() {
				b := maskAddr.As4()
				mask := net.IPv4Mask(b[0], b[1], b[2], b[3])
				ones, bits := mask.Size()
				if bits == 32 && ones >= 0 {
					return netip.PrefixFrom(ip, ones).Masked(), true
				}
			}
		}
	}
	return netip.Prefix{}, false
}

func ipServiceAddressExcludesConnection(address string, extra *ExtraParams) bool {
	if extra == nil {
		return false
	}

	var prefixes []netip.Prefix
	for _, raw := range strings.Split(address, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		if p, ok := parseIpServiceEntry(part); ok {
			prefixes = append(prefixes, p)
		}
	}
	if len(prefixes) == 0 {
		return false
	}

	var targetIP netip.Addr
	if parsed, err := netip.ParseAddr(strings.TrimSpace(extra.ActiveHost)); err == nil {
		targetIP = parsed.Unmap()
		familyMatch := false
		for _, p := range prefixes {
			if (targetIP.Is4() && p.Addr().Is4()) || (targetIP.Is6() && p.Addr().Is6()) {
				familyMatch = true
				break
			}
		}
		if !familyMatch {
			return true
		}
	}

	var srcIP netip.Addr
	if strings.TrimSpace(extra.ClientSourceIP) != "" {
		if parsed, err := netip.ParseAddr(strings.TrimSpace(extra.ClientSourceIP)); err == nil {
			srcIP = parsed.Unmap()
		}
	} else {
		srcIP = detectLocalIPForHost(extra.ActiveHost, extra.ActivePort)
	}

	enforceSourceCheck := srcIP.IsValid() && (strings.TrimSpace(extra.ClientSourceIP) != "" ||
		srcIP.IsLoopback() ||
		(targetIP.IsValid() && (targetIP.IsLoopback() ||
			((srcIP.IsPrivate() || srcIP.IsLinkLocalUnicast()) && (targetIP.IsPrivate() || targetIP.IsLinkLocalUnicast())))))

	if enforceSourceCheck {
		for _, p := range prefixes {
			if p.Contains(srcIP) {
				return false
			}
		}
		return true
	}

	return false
}

func detectLocalIPForHost(host string, port int) netip.Addr {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return netip.Addr{}
	}

	conn, err := net.DialTimeout("udp", net.JoinHostPort(host, strconv.Itoa(port)), time.Second)
	if err != nil {
		return netip.Addr{}
	}
	defer conn.Close()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return netip.Addr{}
	}
	addr, ok := netip.AddrFromSlice(udpAddr.IP)
	if !ok {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func isMacWinboxDisabledList(v string) bool {
	trimmed := strings.TrimSpace(v)
	return trimmed == "" || strings.EqualFold(trimmed, "none")
}

func checkMacWinboxLockout(_ context.Context, hasPriorState bool, priorList string, newList string, ack string, c Client) error {
	if !isMacWinboxDisabledList(newList) {
		return nil
	}

	var prevList string
	if hasPriorState {
		prevList = strings.TrimSpace(priorList)
		if isMacWinboxDisabledList(prevList) {
			return nil
		}
	} else {
		if c == nil {
			return nil
		}
		live := MikrotikItem{}
		err := c.SendRequest(crudRead, &URL{Path: "/tool/mac-server/mac-winbox"}, nil, &live)
		if err != nil {
			return fmt.Errorf("failed to verify live MAC WinBox recovery path before applying %q: %w", newList, err)
		}
		prevList = strings.TrimSpace(live["allowed-interface-list"])
		if isMacWinboxDisabledList(prevList) {
			return nil
		}
	}

	expectedToken := fmt.Sprintf("mac-winbox:%s->none", prevList)
	if strings.TrimSpace(ack) == expectedToken {
		return nil
	}

	return fmt.Errorf(
		"refusing to disable MAC WinBox recovery path (allowed_interface_list changing from %q to %q); if intentional, set lockout_ack = %q",
		prevList, newList, expectedToken,
	)
}

// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	// MNDPPort is the UDP port used by MikroTik Neighbor Discovery Protocol.
	MNDPPort = 5678

	mndpHeaderLen        = 4
	mndpProbeInterval    = 500 * time.Millisecond
	mndpDefaultTimeout   = 5 * time.Second
	mndpReadBufferSize   = 4096
	mndpTLVMacAddress    = 1
	mndpTLVIdentity      = 5
	mndpTLVVersion       = 7
	mndpTLVPlatform      = 8
	mndpTLVUptime        = 10
	mndpTLVSoftwareID    = 11
	mndpTLVBoard         = 12
	mndpTLVUnpack        = 14
	mndpTLVIPv6Address   = 15
	mndpTLVInterfaceName = 16
	mndpTLVIPv4Address   = 17
)

var mndpProbePayload = []byte{0x00, 0x00, 0x00, 0x00}

// MNDPNeighbor represents a single RouterOS neighbor discovered via MNDP.
type MNDPNeighbor struct {
	MACAddress    string `json:"mac_address"`
	Identity      string `json:"identity,omitempty"`
	Version       string `json:"version,omitempty"`
	Platform      string `json:"platform,omitempty"`
	UptimeSeconds uint32 `json:"uptime_seconds,omitempty"`
	SoftwareID    string `json:"software_id,omitempty"`
	Board         string `json:"board,omitempty"`
	Unpack        uint8  `json:"unpack,omitempty"`
	IPv6Address   string `json:"ipv6_address,omitempty"`
	InterfaceName string `json:"interface_name,omitempty"`
	IPv4Address   string `json:"ipv4_address,omitempty"`
	SourceIP      string `json:"source_ip,omitempty"`
}

// DiscoverOptions configures an MNDP discovery run.
type DiscoverOptions struct {
	Interface string
	Timeout   time.Duration
	StrictPin bool

	// Optional overrides used primarily in tests.
	ProbeAddr string
	LocalPort int
}

// DecodeMNDPPacket decodes a raw MNDP UDP payload into an MNDPNeighbor with
// strict bounds checking on every TLV.
func DecodeMNDPPacket(data []byte) (MNDPNeighbor, error) {
	if len(data) < mndpHeaderLen {
		return MNDPNeighbor{}, fmt.Errorf("MNDP packet too short: %d bytes", len(data))
	}
	if len(data) == mndpHeaderLen {
		return MNDPNeighbor{}, errors.New("MNDP packet contains no TLVs")
	}

	var n MNDPNeighbor
	offset := mndpHeaderLen
	for offset < len(data) {
		if len(data)-offset < 4 {
			return MNDPNeighbor{}, fmt.Errorf("truncated MNDP TLV header at offset %d", offset)
		}
		tlvType := binary.BigEndian.Uint16(data[offset : offset+2])
		tlvLen := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		offset += 4
		if tlvLen > len(data)-offset {
			return MNDPNeighbor{}, fmt.Errorf("truncated MNDP TLV (type=%d, len=%d) at offset %d", tlvType, tlvLen, offset)
		}
		val := data[offset : offset+tlvLen]
		offset += tlvLen

		switch tlvType {
		case mndpTLVMacAddress:
			if len(val) != 6 {
				return MNDPNeighbor{}, fmt.Errorf("invalid MAC address TLV length %d", len(val))
			}
			n.MACAddress = formatMACUpper(val)
		case mndpTLVIdentity:
			s, err := decodeMNDPString(val, "identity")
			if err != nil {
				return MNDPNeighbor{}, err
			}
			n.Identity = s
		case mndpTLVVersion:
			s, err := decodeMNDPString(val, "version")
			if err != nil {
				return MNDPNeighbor{}, err
			}
			n.Version = s
		case mndpTLVPlatform:
			s, err := decodeMNDPString(val, "platform")
			if err != nil {
				return MNDPNeighbor{}, err
			}
			n.Platform = s
		case mndpTLVUptime:
			if len(val) != 4 {
				return MNDPNeighbor{}, fmt.Errorf("invalid uptime TLV length %d", len(val))
			}
			n.UptimeSeconds = binary.LittleEndian.Uint32(val)
		case mndpTLVSoftwareID:
			s, err := decodeMNDPString(val, "software_id")
			if err != nil {
				return MNDPNeighbor{}, err
			}
			n.SoftwareID = s
		case mndpTLVBoard:
			s, err := decodeMNDPString(val, "board")
			if err != nil {
				return MNDPNeighbor{}, err
			}
			n.Board = s
		case mndpTLVUnpack:
			if len(val) != 1 {
				return MNDPNeighbor{}, fmt.Errorf("invalid unpack TLV length %d", len(val))
			}
			n.Unpack = val[0]
		case mndpTLVIPv6Address:
			if len(val) != 16 {
				return MNDPNeighbor{}, fmt.Errorf("invalid IPv6 address TLV length %d", len(val))
			}
			n.IPv6Address = net.IP(val).String()
		case mndpTLVInterfaceName:
			s, err := decodeMNDPString(val, "interface_name")
			if err != nil {
				return MNDPNeighbor{}, err
			}
			n.InterfaceName = s
		case mndpTLVIPv4Address:
			if len(val) != 4 {
				return MNDPNeighbor{}, fmt.Errorf("invalid IPv4 address TLV length %d", len(val))
			}
			n.IPv4Address = net.IP(val).String()
		default:
			// Unknown TLVs are ignored safely after bounds verification.
		}
	}

	if n.MACAddress == "" {
		return MNDPNeighbor{}, errors.New("MNDP packet missing MAC address TLV")
	}
	return n, nil
}

func decodeMNDPString(val []byte, field string) (string, error) {
	trimmed := strings.TrimRight(string(val), "\x00")
	if !utf8.ValidString(trimmed) {
		return "", fmt.Errorf("invalid UTF-8 in MNDP %s TLV", field)
	}
	return trimmed, nil
}

func formatMACUpper(mac []byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// DiscoverMNDP binds a UDP socket to opts.Interface, broadcasts MNDP probes,
// and returns deduplicated neighbors sorted ascending by MACAddress.
func DiscoverMNDP(ctx context.Context, opts DiscoverOptions) ([]MNDPNeighbor, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = mndpDefaultTimeout
	}

	probeAddrStr := opts.ProbeAddr
	if probeAddrStr == "" {
		probeAddrStr = fmt.Sprintf("255.255.255.255:%d", MNDPPort)
	}
	probeAddr, err := net.ResolveUDPAddr("udp4", probeAddrStr)
	if err != nil {
		return nil, fmt.Errorf("resolve probe address %q: %w", probeAddrStr, err)
	}

	localPort := opts.LocalPort
	if localPort == 0 && opts.ProbeAddr == "" {
		localPort = MNDPPort
	}

	conn, err := openInterfaceUDP(ctx, opts.Interface, localPort, opts.StrictPin)
	if err != nil && localPort == MNDPPort && errors.Is(err, syscall.EADDRINUSE) {
		conn, err = openInterfaceUDP(ctx, opts.Interface, 0, opts.StrictPin)
	}
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return discoverMNDPOnConn(ctx, conn, probeAddr, timeout)
}

func discoverMNDPOnConn(ctx context.Context, conn boundPacketConn, probeAddr *net.UDPAddr, timeout time.Duration) ([]MNDPNeighbor, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Also send probes from an ephemeral socket on the same interface if our
	// main socket is listening on 5678, or directly from conn.
	if _, err := conn.WriteTo(mndpProbePayload, probeAddr); err != nil {
		return nil, fmt.Errorf("send MNDP probe: %w", err)
	}

	stopProbe := make(chan struct{})
	defer close(stopProbe)
	go func() {
		ticker := time.NewTicker(mndpProbeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopProbe:
				return
			case <-ticker.C:
				_, _ = conn.WriteTo(mndpProbePayload, probeAddr)
			}
		}
	}()

	byMAC := make(map[string]MNDPNeighbor)
	buf := make([]byte, mndpReadBufferSize)

	for {
		select {
		case <-ctx.Done():
			return sortedNeighbors(byMAC), nil
		default:
		}

		deadline := time.Now().Add(200 * time.Millisecond)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set read deadline: %w", err)
		}

		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				if ctx.Err() != nil {
					return sortedNeighbors(byMAC), nil
				}
				continue
			}
			if ctx.Err() != nil {
				return sortedNeighbors(byMAC), nil
			}
			return nil, fmt.Errorf("read MNDP packet: %w", err)
		}

		neighbor, decErr := DecodeMNDPPacket(buf[:n])
		if decErr != nil {
			// Ignore malformed/probe loopback packets during passive discovery.
			continue
		}
		if udpAddr, ok := addr.(*net.UDPAddr); ok && udpAddr.IP != nil {
			neighbor.SourceIP = udpAddr.IP.String()
		}

		if existing, exists := byMAC[neighbor.MACAddress]; exists {
			byMAC[neighbor.MACAddress] = mergeNeighbor(existing, neighbor)
		} else {
			byMAC[neighbor.MACAddress] = neighbor
		}
	}
}

func mergeNeighbor(a, b MNDPNeighbor) MNDPNeighbor {
	if b.Identity != "" {
		a.Identity = b.Identity
	}
	if b.Version != "" {
		a.Version = b.Version
	}
	if b.Platform != "" {
		a.Platform = b.Platform
	}
	if b.UptimeSeconds != 0 {
		a.UptimeSeconds = b.UptimeSeconds
	}
	if b.SoftwareID != "" {
		a.SoftwareID = b.SoftwareID
	}
	if b.Board != "" {
		a.Board = b.Board
	}
	if b.Unpack != 0 {
		a.Unpack = b.Unpack
	}
	if b.IPv6Address != "" {
		a.IPv6Address = b.IPv6Address
	}
	if b.InterfaceName != "" {
		a.InterfaceName = b.InterfaceName
	}
	if b.IPv4Address != "" {
		a.IPv4Address = b.IPv4Address
	}
	if b.SourceIP != "" {
		a.SourceIP = b.SourceIP
	}
	return a
}

func sortedNeighbors(m map[string]MNDPNeighbor) []MNDPNeighbor {
	out := make([]MNDPNeighbor, 0, len(m))
	for _, n := range m {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].MACAddress < out[j].MACAddress
	})
	return out
}

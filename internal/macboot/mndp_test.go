// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func buildTestMNDPPacket(tlvs ...struct {
	typ uint16
	val []byte
}) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x01, 0x00})
	for _, tlv := range tlvs {
		var hdr [4]byte
		binary.BigEndian.PutUint16(hdr[0:2], tlv.typ)
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(tlv.val)))
		buf.Write(hdr[:])
		buf.Write(tlv.val)
	}
	return buf.Bytes()
}

func TestDecodeMNDPPacket(t *testing.T) {
	var uptime [4]byte
	binary.LittleEndian.PutUint32(uptime[:], 123456)

	pkt := buildTestMNDPPacket(
		struct {
			typ uint16
			val []byte
		}{mndpTLVMacAddress, []byte{0x54, 0x05, 0xab, 0x01, 0x02, 0x03}},
		struct {
			typ uint16
			val []byte
		}{mndpTLVIdentity, []byte("MikroTik")},
		struct {
			typ uint16
			val []byte
		}{mndpTLVVersion, []byte("7.25.2 (stable)")},
		struct {
			typ uint16
			val []byte
		}{mndpTLVPlatform, []byte("MikroTik")},
		struct {
			typ uint16
			val []byte
		}{mndpTLVUptime, uptime[:]},
		struct {
			typ uint16
			val []byte
		}{mndpTLVSoftwareID, []byte("ABC1-DEF2")},
		struct {
			typ uint16
			val []byte
		}{mndpTLVBoard, []byte("CHR")},
		struct {
			typ uint16
			val []byte
		}{mndpTLVUnpack, []byte{1}},
		struct {
			typ uint16
			val []byte
		}{mndpTLVIPv6Address, net.ParseIP("fe80::5605:abff:fe01:203").To16()},
		struct {
			typ uint16
			val []byte
		}{mndpTLVInterfaceName, []byte("ether2")},
		struct {
			typ uint16
			val []byte
		}{mndpTLVIPv4Address, net.ParseIP("172.30.99.2").To4()},
		struct {
			typ uint16
			val []byte
		}{999, []byte{0xde, 0xad, 0xbe, 0xef}},
	)

	got, err := DecodeMNDPPacket(pkt)
	if err != nil {
		t.Fatalf("DecodeMNDPPacket failed: %v", err)
	}

	if got.MACAddress != "54:05:AB:01:02:03" {
		t.Fatalf("MACAddress = %q", got.MACAddress)
	}
	if got.Identity != "MikroTik" || got.Version != "7.25.2 (stable)" || got.Platform != "MikroTik" {
		t.Fatalf("unexpected identity/version/platform: %+v", got)
	}
	if got.UptimeSeconds != 123456 {
		t.Fatalf("UptimeSeconds = %d, want 123456", got.UptimeSeconds)
	}
	if got.SoftwareID != "ABC1-DEF2" || got.Board != "CHR" || got.Unpack != 1 {
		t.Fatalf("unexpected software/board/unpack: %+v", got)
	}
	if got.IPv6Address != "fe80::5605:abff:fe01:203" || got.InterfaceName != "ether2" || got.IPv4Address != "172.30.99.2" {
		t.Fatalf("unexpected network fields: %+v", got)
	}

	invalidCases := [][]byte{
		{},
		{0, 0, 0},
		{0, 0, 0, 0},       // empty probe
		{0, 0, 0, 0, 0, 1}, // truncated TLV header
		{0, 0, 0, 0, 0, 1, 0, 6, 0x01, 0x02},
		buildTestMNDPPacket(struct {
			typ uint16
			val []byte
		}{mndpTLVMacAddress, []byte{1, 2, 3}}),
		buildTestMNDPPacket(struct {
			typ uint16
			val []byte
		}{mndpTLVIdentity, []byte("no-mac")}),
		buildTestMNDPPacket(
			struct {
				typ uint16
				val []byte
			}{mndpTLVMacAddress, []byte{1, 2, 3, 4, 5, 6}},
			struct {
				typ uint16
				val []byte
			}{mndpTLVUptime, []byte{1, 2}},
		),
	}
	for i, bad := range invalidCases {
		if _, err := DecodeMNDPPacket(bad); err == nil {
			t.Fatalf("case %d: expected DecodeMNDPPacket error, got nil", i)
		}
	}
}

func TestDiscoverMNDPOnConnDeduplicatesAndSorts(t *testing.T) {
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer srv.Close()

	cli, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen cli udp: %v", err)
	}
	defer cli.Close()

	go func() {
		buf := make([]byte, 256)
		n, raddr, err := srv.ReadFromUDP(buf)
		if err != nil || n != 4 {
			return
		}
		pktB := buildTestMNDPPacket(
			struct {
				typ uint16
				val []byte
			}{mndpTLVMacAddress, []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x02}},
			struct {
				typ uint16
				val []byte
			}{mndpTLVIdentity, []byte("router-b")},
		)
		pktA1 := buildTestMNDPPacket(
			struct {
				typ uint16
				val []byte
			}{mndpTLVMacAddress, []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x01}},
			struct {
				typ uint16
				val []byte
			}{mndpTLVIdentity, []byte("router-a")},
		)
		pktA2 := buildTestMNDPPacket(
			struct {
				typ uint16
				val []byte
			}{mndpTLVMacAddress, []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x01}},
			struct {
				typ uint16
				val []byte
			}{mndpTLVVersion, []byte("7.25.2")},
		)
		_, _ = srv.WriteToUDP(pktB, raddr)
		_, _ = srv.WriteToUDP(pktA1, raddr)
		_, _ = srv.WriteToUDP(pktA2, raddr)
	}()

	bound := &stdBoundConn{UDPConn: cli}
	neighbors, err := discoverMNDPOnConn(context.Background(), bound, srv.LocalAddr().(*net.UDPAddr), 300*time.Millisecond)
	if err != nil {
		t.Fatalf("discoverMNDPOnConn failed: %v", err)
	}
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 deduplicated neighbors, got %d: %+v", len(neighbors), neighbors)
	}
	if neighbors[0].MACAddress != "AA:BB:CC:00:00:01" || neighbors[0].Identity != "router-a" || neighbors[0].Version != "7.25.2" {
		t.Fatalf("unexpected first neighbor: %+v", neighbors[0])
	}
	if neighbors[1].MACAddress != "AA:BB:CC:00:00:02" || neighbors[1].Identity != "router-b" {
		t.Fatalf("unexpected second neighbor: %+v", neighbors[1])
	}
}

func FuzzDecodeMNDP(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Add(buildTestMNDPPacket(
		struct {
			typ uint16
			val []byte
		}{mndpTLVMacAddress, []byte{0x54, 0x05, 0xab, 0x01, 0x02, 0x03}},
		struct {
			typ uint16
			val []byte
		}{mndpTLVIdentity, []byte("MikroTik")},
	))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeMNDPPacket(data)
	})
}

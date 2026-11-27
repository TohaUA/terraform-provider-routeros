//go:build linux

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
	"syscall"
	"time"
	"unsafe"
)

func setCommonUDPSocketOptions(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return fmt.Errorf("set SO_REUSEADDR: %w", err)
	}
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); err != nil {
		return fmt.Errorf("set SO_BROADCAST: %w", err)
	}
	return nil
}

func bindSocketToInterface(fd uintptr, ifaceName string, _ int) error {
	return syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifaceName)
}

type linuxPktInfoConn struct {
	conn    *net.UDPConn
	iface   *net.Interface
	warning string
}

func (c *linuxPktInfoConn) Interface() *net.Interface         { return c.iface }
func (c *linuxPktInfoConn) Warning() string                   { return c.warning }
func (c *linuxPktInfoConn) LocalAddr() net.Addr               { return c.conn.LocalAddr() }
func (c *linuxPktInfoConn) SetDeadline(t time.Time) error     { return c.conn.SetDeadline(t) }
func (c *linuxPktInfoConn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }
func (c *linuxPktInfoConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
func (c *linuxPktInfoConn) Close() error { return c.conn.Close() }

func (c *linuxPktInfoConn) ReadFrom(b []byte) (int, net.Addr, error) {
	oob := make([]byte, 256)
	for {
		n, oobn, _, addr, err := c.conn.ReadMsgUDP(b, oob)
		if err != nil {
			return n, addr, err
		}
		ifidx, ok := extractPktInfoIfIndex(oob[:oobn])
		if ok && ifidx != c.iface.Index {
			continue
		}
		return n, addr, nil
	}
}

func (c *linuxPktInfoConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("expected *net.UDPAddr, got %T", addr)
	}
	oob := encodePktInfoOOB(c.iface.Index)
	n, _, err := c.conn.WriteMsgUDP(b, oob, udpAddr)
	return n, err
}

func encodePktInfoOOB(ifIndex int) []byte {
	var pktinfo [12]byte
	binary.NativeEndian.PutUint32(pktinfo[0:4], uint32(ifIndex))

	hdrLen := syscall.CmsgSpace(len(pktinfo))
	buf := make([]byte, hdrLen)
	hdr := (*syscall.Cmsghdr)(unsafe.Pointer(&buf[0]))
	hdr.Level = syscall.IPPROTO_IP
	hdr.Type = syscall.IP_PKTINFO
	hdr.SetLen(syscall.CmsgLen(len(pktinfo)))
	copy(buf[syscall.CmsgLen(0):], pktinfo[:])
	return buf
}

func extractPktInfoIfIndex(oob []byte) (int, bool) {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, false
	}
	for _, m := range msgs {
		if m.Header.Level == syscall.IPPROTO_IP && m.Header.Type == syscall.IP_PKTINFO && len(m.Data) >= 4 {
			idx := int(int32(binary.NativeEndian.Uint32(m.Data[0:4])))
			return idx, true
		}
	}
	return 0, false
}

func openUnprivilegedFallback(ctx context.Context, iface *net.Interface, ipv4 net.IP, localPort int) (boundPacketConn, error) {
	if iface == nil || ipv4 == nil {
		return nil, errors.New("fallback requires an interface with an IPv4 address")
	}

	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctrlErr error
			err := c.Control(func(fd uintptr) {
				if err := setCommonUDPSocketOptions(fd); err != nil {
					ctrlErr = err
					return
				}
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_PKTINFO, 1); err != nil {
					ctrlErr = fmt.Errorf("set IP_PKTINFO: %w", err)
					return
				}
			})
			if err != nil {
				return err
			}
			return ctrlErr
		},
	}

	pc, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf("0.0.0.0:%d", localPort))
	if err != nil {
		return nil, err
	}
	udpConn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("unexpected packet conn type %T", pc)
	}
	return &linuxPktInfoConn{
		conn:  udpConn,
		iface: iface,
		warning: fmt.Sprintf(
			"SO_BINDTODEVICE denied (EPERM) on %q; fell back to IP_PKTINFO pinning via %s (grant CAP_NET_RAW for strict kernel socket pinning)",
			iface.Name, ipv4.String(),
		),
	}, nil
}

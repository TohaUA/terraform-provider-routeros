// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

var (
	errInterfacePinUnsupported = errors.New("interface-pinned UDP socket binding is not supported on this operating system")
)

type boundPacketConn interface {
	ReadFrom(b []byte) (int, net.Addr, error)
	WriteTo(b []byte, addr net.Addr) (int, error)
	LocalAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
	Interface() *net.Interface
	Warning() string
}

type stdBoundConn struct {
	*net.UDPConn
	iface   *net.Interface
	warning string
}

func (c *stdBoundConn) Interface() *net.Interface { return c.iface }
func (c *stdBoundConn) Warning() string           { return c.warning }

func openInterfaceUDP(ctx context.Context, ifaceName string, localPort int, strictPin bool) (boundPacketConn, error) {
	if ifaceName == "" {
		return nil, errors.New("interface name must not be empty")
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %q: %w", ifaceName, err)
	}

	if iface.Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("interface %q is down", ifaceName)
	}

	var bindErr error
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctrlErr error
			err := c.Control(func(fd uintptr) {
				if err := setCommonUDPSocketOptions(fd); err != nil {
					ctrlErr = err
					return
				}
				if err := bindSocketToInterface(fd, iface.Name, iface.Index); err != nil {
					bindErr = err
					ctrlErr = err
				}
			})
			if err != nil {
				return err
			}
			return ctrlErr
		},
	}

	listenAddr := fmt.Sprintf("0.0.0.0:%d", localPort)
	pc, err := lc.ListenPacket(ctx, "udp4", listenAddr)
	if err == nil {
		udpConn, ok := pc.(*net.UDPConn)
		if !ok {
			_ = pc.Close()
			return nil, fmt.Errorf("unexpected packet conn type %T", pc)
		}
		return &stdBoundConn{UDPConn: udpConn, iface: iface}, nil
	}

	if bindErr != nil && (errors.Is(bindErr, syscall.EPERM) || errors.Is(bindErr, syscall.EACCES)) {
		hasIPv4, ipv4 := interfaceIPv4(iface)
		if !strictPin && hasIPv4 {
			if fbConn, fbErr := openUnprivilegedFallback(ctx, iface, ipv4, localPort); fbErr == nil {
				return fbConn, nil
			}
		}
		return nil, FormatSocketPermissionError(iface.Name, hasIPv4, bindErr)
	}

	return nil, fmt.Errorf("bind UDP socket on interface %q (%s): %w", iface.Name, listenAddr, err)
}

func interfaceIPv4(iface *net.Interface) (bool, net.IP) {
	addrs, err := iface.Addrs()
	if err != nil {
		return false, nil
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 != nil && !ip4.IsUnspecified() {
			return true, ip4
		}
	}
	return false, nil
}

// FormatSocketPermissionError returns a diagnostic error explaining why
// interface-pinned UDP binding failed and how to grant CAP_NET_RAW or run with
// sufficient privileges.
func FormatSocketPermissionError(ifaceName string, hasIPv4 bool, err error) error {
	if hasIPv4 {
		return fmt.Errorf(
			"failed to pin UDP socket to interface %q (%w): non-root users on Linux need CAP_NET_RAW (for example, `sudo setcap cap_net_raw+ep <binary>` or running under sudo) for strict SO_BINDTODEVICE pinning",
			ifaceName, err,
		)
	}
	return fmt.Errorf(
		"failed to pin UDP socket to unaddressed interface %q (%w): SO_BINDTODEVICE requires CAP_NET_RAW or root privileges when the interface has no IPv4 address; run with `sudo` or grant CAP_NET_RAW (`sudo setcap cap_net_raw+ep <binary>`)",
		ifaceName, err,
	)
}

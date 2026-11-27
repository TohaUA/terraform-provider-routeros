//go:build darwin

// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"context"
	"fmt"
	"net"
	"syscall"
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

func bindSocketToInterface(fd uintptr, _ string, ifaceIndex int) error {
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BOUND_IF, ifaceIndex)
}

func openUnprivilegedFallback(_ context.Context, _ *net.Interface, _ net.IP, _ int) (boundPacketConn, error) {
	return nil, errInterfacePinUnsupported
}

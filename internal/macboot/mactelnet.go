// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// MACTelnetPort is the UDP port used by MikroTik MAC-Telnet.
	MACTelnetPort       = 20561
	macTelnetHeaderLen  = 22
	macTelnetVersion    = 1
	macTelnetClientType = 0x0015

	macTelnetPTypeStartSession uint8 = 0
	macTelnetPTypeData         uint8 = 1
	macTelnetPTypeAck          uint8 = 2
	macTelnetPTypePing         uint8 = 4
	macTelnetPTypePong         uint8 = 5
	macTelnetPTypeEndSession   uint8 = 255

	macTelnetCPBeginAuth     uint8 = 0
	macTelnetCPEncryptionKey uint8 = 1
	macTelnetCPPassword      uint8 = 2
	macTelnetCPUsername      uint8 = 3
	macTelnetCPTermType      uint8 = 4
	macTelnetCPTermWidth     uint8 = 5
	macTelnetCPTermHeight    uint8 = 6
	macTelnetCPEndAuth       uint8 = 9

	macTelnetTermWidth  uint16 = 160
	macTelnetTermHeight uint16 = 50

	macTelnetRetransmitInterval = 300 * time.Millisecond
	macTelnetKeepaliveInterval  = 5 * time.Second
)

var (
	macTelnetCPMagic = [4]byte{0x56, 0x34, 0x12, 0xff}
	ansiEscapeRegexp = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b[@-Z\\-_]`)
	rosPromptRegexp  = regexp.MustCompile(`(?:^|\n)\[[^\r\n\]]+@[^\r\n\]]+\][^\r\n>]*>[ \t]*\z`)
)

// MACTelnetOptions configures a MAC-Telnet client session.
type MACTelnetOptions struct {
	Interface      string
	TargetMAC      string
	Username       string
	Password       string
	Timeout        time.Duration
	StrictPin      bool
	UnsafeRawDebug bool
	Logger         func(format string, args ...any)

	// Optional overrides for tests.
	RemoteAddr string
	LocalPort  int
}

// MACTelnetPacket represents a decoded 22-byte MAC-Telnet frame and its payload.
type MACTelnetPacket struct {
	Version    uint8
	PType      uint8
	SrcMAC     [6]byte
	DstMAC     [6]byte
	SesKey     uint16
	ClientType uint16
	Counter    uint32
	Payload    []byte
}

// ControlPacket represents a 9-byte-header MAC-Telnet control block.
type ControlPacket struct {
	Type uint8
	Data []byte
}

// EncodeMACTelnetPacket encodes a 22-byte MAC-Telnet header and optional payload.
func EncodeMACTelnetPacket(toServer bool, ptype uint8, srcMAC, dstMAC [6]byte, sesKey, clientType uint16, counter uint32, payload []byte) []byte {
	buf := make([]byte, macTelnetHeaderLen+len(payload))
	buf[0] = macTelnetVersion
	buf[1] = ptype
	copy(buf[2:8], srcMAC[:])
	copy(buf[8:14], dstMAC[:])
	if toServer {
		binary.BigEndian.PutUint16(buf[14:16], sesKey)
		binary.BigEndian.PutUint16(buf[16:18], clientType)
	} else {
		binary.BigEndian.PutUint16(buf[14:16], clientType)
		binary.BigEndian.PutUint16(buf[16:18], sesKey)
	}
	binary.BigEndian.PutUint32(buf[18:22], counter)
	copy(buf[22:], payload)
	return buf
}

// DecodeMACTelnetPacket decodes a MAC-Telnet frame. When fromServer is true,
// bytes 14..17 are parsed according to Server->Client layout (clienttype, seskey)
// while also tolerating unswapped layout when clientType == 0x0015 at [16:18].
func DecodeMACTelnetPacket(data []byte, fromServer bool, expectedSesKey uint16) (MACTelnetPacket, error) {
	if len(data) < macTelnetHeaderLen {
		return MACTelnetPacket{}, fmt.Errorf("MAC-Telnet packet too short: %d bytes", len(data))
	}
	if data[0] != macTelnetVersion {
		return MACTelnetPacket{}, fmt.Errorf("unsupported MAC-Telnet version %d", data[0])
	}
	var pkt MACTelnetPacket
	pkt.Version = data[0]
	pkt.PType = data[1]
	copy(pkt.SrcMAC[:], data[2:8])
	copy(pkt.DstMAC[:], data[8:14])
	f14 := binary.BigEndian.Uint16(data[14:16])
	f16 := binary.BigEndian.Uint16(data[16:18])
	if fromServer {
		if expectedSesKey != 0 && f14 == expectedSesKey && f16 == macTelnetClientType {
			pkt.SesKey = f14
			pkt.ClientType = f16
		} else {
			pkt.ClientType = f14
			pkt.SesKey = f16
		}
	} else {
		pkt.SesKey = f14
		pkt.ClientType = f16
	}
	pkt.Counter = binary.BigEndian.Uint32(data[18:22])
	if len(data) > macTelnetHeaderLen {
		pkt.Payload = append([]byte(nil), data[macTelnetHeaderLen:]...)
	}
	return pkt, nil
}

// EncodeControlPacket encodes a single MAC-Telnet control block.
func EncodeControlPacket(cpType uint8, data []byte) []byte {
	buf := make([]byte, 9+len(data))
	copy(buf[0:4], macTelnetCPMagic[:])
	buf[4] = cpType
	binary.BigEndian.PutUint32(buf[5:9], uint32(len(data)))
	copy(buf[9:], data)
	return buf
}

// ParseDataPayload splits a MAC-Telnet DATA payload into control packets and plain terminal text.
func ParseDataPayload(payload []byte) ([]ControlPacket, []byte, error) {
	var ctrls []ControlPacket
	var plain bytes.Buffer

	offset := 0
	for offset < len(payload) {
		if len(payload)-offset >= 9 && bytes.Equal(payload[offset:offset+4], macTelnetCPMagic[:]) {
			cpType := payload[offset+4]
			cpLen := binary.BigEndian.Uint32(payload[offset+5 : offset+9])
			offset += 9
			if uint64(cpLen) > uint64(len(payload)-offset) {
				return nil, nil, fmt.Errorf("truncated MAC-Telnet control block (type=%d, len=%d)", cpType, cpLen)
			}
			cpData := append([]byte(nil), payload[offset:offset+int(cpLen)]...)
			ctrls = append(ctrls, ControlPacket{Type: cpType, Data: cpData})
			offset += int(cpLen)
			continue
		}

		idx := bytes.Index(payload[offset:], macTelnetCPMagic[:])
		if idx < 0 {
			plain.Write(payload[offset:])
			break
		}
		if idx == 0 {
			// Fewer than 9 bytes remained after magic; treat as plain bytes.
			plain.Write(payload[offset:])
			break
		}
		plain.Write(payload[offset : offset+idx])
		offset += idx
	}

	return ctrls, plain.Bytes(), nil
}

// MACTelnetSession manages an authenticated MAC-Telnet session over UDP.
type MACTelnetSession struct {
	conn           boundPacketConn
	remoteAddr     *net.UDPAddr
	localMAC       [6]byte
	targetMAC      [6]byte
	sesKey         uint16
	unsafeRawDebug bool
	logger         func(format string, args ...any)
	timeout        time.Duration

	mu           sync.Mutex
	txCounter    uint32
	rxCounter    uint32
	ackCounter   uint32
	ackReceived  bool
	ackNotify    chan struct{}
	termNotify   chan struct{}
	termBuf      bytes.Buffer
	ctrlCh       chan ControlPacket
	closed       bool
	remoteClosed bool
	doneCh       chan struct{}
	closeOnce    sync.Once
}

// DialMACTelnet opens an interface-pinned MAC-Telnet session and authenticates
// with EC-SRP5.
func DialMACTelnet(ctx context.Context, opts MACTelnetOptions) (*MACTelnetSession, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	targetHW, err := net.ParseMAC(strings.TrimSpace(opts.TargetMAC))
	if err != nil || len(targetHW) != 6 {
		return nil, fmt.Errorf("invalid target MAC address %q", opts.TargetMAC)
	}
	var targetMAC [6]byte
	copy(targetMAC[:], targetHW)

	remoteAddrStr := opts.RemoteAddr
	if remoteAddrStr == "" {
		remoteAddrStr = fmt.Sprintf("255.255.255.255:%d", MACTelnetPort)
	}
	remoteAddr, err := net.ResolveUDPAddr("udp4", remoteAddrStr)
	if err != nil {
		return nil, fmt.Errorf("resolve remote address %q: %w", remoteAddrStr, err)
	}

	conn, err := openInterfaceUDP(ctx, opts.Interface, opts.LocalPort, opts.StrictPin)
	if err != nil {
		return nil, err
	}

	var localMAC [6]byte
	if hw := conn.Interface().HardwareAddr; len(hw) == 6 {
		copy(localMAC[:], hw)
	} else {
		localMAC = [6]byte{0x02, 0x4d, 0x42, 0x00, 0x00, 0x01}
	}

	sesKey, err := randomSessionKey()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	sess := &MACTelnetSession{
		conn:           conn,
		remoteAddr:     remoteAddr,
		localMAC:       localMAC,
		targetMAC:      targetMAC,
		sesKey:         sesKey,
		unsafeRawDebug: opts.UnsafeRawDebug,
		logger:         opts.Logger,
		timeout:        opts.Timeout,
		ackNotify:      make(chan struct{}, 64),
		termNotify:     make(chan struct{}, 64),
		ctrlCh:         make(chan ControlPacket, 64),
		doneCh:         make(chan struct{}),
	}

	if w := conn.Warning(); w != "" {
		sess.logf("warning: %s", w)
	}
	sess.logf("connecting MAC-Telnet session to %s via %s (seskey=0x%04x, username=%q, password=[REDACTED])",
		formatMACUpper(targetMAC[:]), opts.Interface, sesKey, opts.Username)

	go sess.readLoop()
	go sess.keepaliveLoop()

	authCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	if err := sess.authenticate(authCtx, opts.Username, opts.Password); err != nil {
		_ = sess.Close()
		return nil, err
	}
	return sess, nil
}

func randomSessionKey() (uint16, error) {
	var b [2]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("generate session key: %w", err)
		}
		k := binary.BigEndian.Uint16(b[:])
		if k != 0 && k != macTelnetClientType {
			return k, nil
		}
	}
}

func (s *MACTelnetSession) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger(format, args...)
	}
}

func (s *MACTelnetSession) debugHex(dir string, raw []byte, redactOffset int, redactLen int) {
	if !s.unsafeRawDebug || s.logger == nil {
		return
	}
	copyBuf := append([]byte(nil), raw...)
	if redactOffset >= 0 && redactLen > 0 && redactOffset+redactLen <= len(copyBuf) {
		for i := redactOffset; i < redactOffset+redactLen; i++ {
			copyBuf[i] = 0
		}
		s.logger("raw %s (%d bytes, secret zeroed): %s", dir, len(raw), hex.EncodeToString(copyBuf))
		return
	}
	s.logger("raw %s (%d bytes): %s", dir, len(raw), hex.EncodeToString(copyBuf))
}

func (s *MACTelnetSession) readLoop() {
	buf := make([]byte, 8192)
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := s.conn.ReadFrom(buf)
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return
		}

		pkt, decErr := DecodeMACTelnetPacket(buf[:n], true, s.sesKey)
		if decErr != nil {
			continue
		}
		if pkt.SrcMAC != s.targetMAC || pkt.SesKey != s.sesKey {
			continue
		}
		s.debugHex("rx", buf[:n], -1, 0)

		switch pkt.PType {
		case macTelnetPTypeAck:
			s.mu.Lock()
			s.ackReceived = true
			if pkt.Counter > s.ackCounter {
				s.ackCounter = pkt.Counter
			}
			s.mu.Unlock()
			select {
			case s.ackNotify <- struct{}{}:
			default:
			}
		case macTelnetPTypePing:
			pong := EncodeMACTelnetPacket(true, macTelnetPTypePong, s.localMAC, s.targetMAC, s.sesKey, macTelnetClientType, pkt.Counter, pkt.Payload)
			s.debugHex("tx-pong", pong, -1, 0)
			_, _ = s.conn.WriteTo(pong, s.remoteAddr)
		case macTelnetPTypeEndSession:
			s.mu.Lock()
			s.remoteClosed = true
			s.mu.Unlock()
			_ = s.Close()
			return
		case macTelnetPTypeData:
			s.handleDataPacket(pkt)
		}
	}
}

func (s *MACTelnetSession) handleDataPacket(pkt MACTelnetPacket) {
	s.mu.Lock()
	expected := s.rxCounter
	pktStart := uint64(pkt.Counter)
	pktEnd := pktStart + uint64(len(pkt.Payload))
	exp64 := uint64(expected)

	var newData []byte
	switch {
	case pktStart == exp64:
		newData = pkt.Payload
	case pktStart < exp64 && pktEnd > exp64:
		newData = pkt.Payload[exp64-pktStart:]
	default:
		// Pure duplicate (pktEnd <= exp64) or out-of-order future packet (pktStart > exp64).
		s.mu.Unlock()
		ack := EncodeMACTelnetPacket(true, macTelnetPTypeAck, s.localMAC, s.targetMAC, s.sesKey, macTelnetClientType, expected, nil)
		s.debugHex("tx-ack-dup", ack, -1, 0)
		_, _ = s.conn.WriteTo(ack, s.remoteAddr)
		return
	}

	s.rxCounter += uint32(len(newData))
	newAck := s.rxCounter
	s.mu.Unlock()

	ack := EncodeMACTelnetPacket(true, macTelnetPTypeAck, s.localMAC, s.targetMAC, s.sesKey, macTelnetClientType, newAck, nil)
	s.debugHex("tx-ack", ack, -1, 0)
	_, _ = s.conn.WriteTo(ack, s.remoteAddr)

	if len(newData) == 0 {
		return
	}

	ctrls, plain, err := ParseDataPayload(newData)
	if err != nil {
		s.logf("malformed MAC-Telnet data payload: %v", err)
		return
	}
	for _, cp := range ctrls {
		select {
		case s.ctrlCh <- cp:
		default:
		}
	}
	if len(plain) > 0 {
		s.mu.Lock()
		s.termBuf.Write(plain)
		s.mu.Unlock()
		select {
		case s.termNotify <- struct{}{}:
		default:
		}
	}
}

func (s *MACTelnetSession) keepaliveLoop() {
	ticker := time.NewTicker(macTelnetKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.doneCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			rx := s.rxCounter
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			ack := EncodeMACTelnetPacket(true, macTelnetPTypeAck, s.localMAC, s.targetMAC, s.sesKey, macTelnetClientType, rx, nil)
			_, _ = s.conn.WriteTo(ack, s.remoteAddr)
		}
	}
}

func (s *MACTelnetSession) sendReliable(ctx context.Context, ptype uint8, payload []byte, redactSecret bool) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("MAC-Telnet session is closed")
	}
	seq := s.txCounter
	targetAck := seq + uint32(len(payload))
	if ptype == macTelnetPTypeData {
		s.txCounter = targetAck
	}
	s.mu.Unlock()

	frame := EncodeMACTelnetPacket(true, ptype, s.localMAC, s.targetMAC, s.sesKey, macTelnetClientType, seq, payload)
	redactOff, redactLen := -1, 0
	if redactSecret && len(payload) > 0 {
		redactOff = macTelnetHeaderLen
		redactLen = len(payload)
	}

	for {
		s.debugHex("tx", frame, redactOff, redactLen)
		if _, err := s.conn.WriteTo(frame, s.remoteAddr); err != nil {
			return fmt.Errorf("send MAC-Telnet packet: %w", err)
		}

		timer := time.NewTimer(macTelnetRetransmitInterval)
		wait := true
		for wait {
			s.mu.Lock()
			acked := false
			if ptype == macTelnetPTypeStartSession {
				acked = s.ackReceived
			} else {
				acked = s.ackCounter >= targetAck
			}
			closed := s.closed
			s.mu.Unlock()
			if acked {
				timer.Stop()
				return nil
			}
			if closed {
				timer.Stop()
				return errors.New("MAC-Telnet session closed while waiting for ACK")
			}

			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("MAC-Telnet ACK timeout: %w", ctx.Err())
			case <-s.doneCh:
				timer.Stop()
				return errors.New("MAC-Telnet remote closed session")
			case <-s.ackNotify:
			case <-timer.C:
				wait = false
			}
		}
	}
}

func (s *MACTelnetSession) authenticate(ctx context.Context, username, password string) error {
	if err := s.sendReliable(ctx, macTelnetPTypeStartSession, nil, false); err != nil {
		return fmt.Errorf("start session: %w", err)
	}

	clientPriv, err := GenPrivateKey()
	if err != nil {
		return fmt.Errorf("generate EC-SRP5 client key: %w", err)
	}
	clientPub, clientParity, err := GenPublicKey(clientPriv)
	if err != nil {
		return fmt.Errorf("generate EC-SRP5 client public key: %w", err)
	}

	var encReq bytes.Buffer
	encReq.WriteString(username)
	encReq.WriteByte(0)
	encReq.Write(clientPub[:])
	encReq.WriteByte(clientParity)

	var step1 bytes.Buffer
	step1.Write(EncodeControlPacket(macTelnetCPBeginAuth, nil))
	step1.Write(EncodeControlPacket(macTelnetCPEncryptionKey, encReq.Bytes()))

	if err := s.sendReliable(ctx, macTelnetPTypeData, step1.Bytes(), false); err != nil {
		return fmt.Errorf("send BeginAuth/EncryptionKey: %w", err)
	}

	// Wait for server EncryptionKey (serverPub[32] || serverParity[1] || salt[16]).
	var serverPub [32]byte
	var serverParity byte
	var salt [16]byte
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for server EC-SRP5 challenge: %w", ctx.Err())
		case <-s.doneCh:
			return errors.New("server closed MAC-Telnet session during authentication")
		case cp := <-s.ctrlCh:
			if cp.Type != macTelnetCPEncryptionKey {
				continue
			}
			if len(cp.Data) != 49 {
				return fmt.Errorf("unexpected server EncryptionKey length %d (expected 49)", len(cp.Data))
			}
			copy(serverPub[:], cp.Data[0:32])
			serverParity = cp.Data[32]
			copy(salt[:], cp.Data[33:49])
			goto challengeReceived
		}
	}

challengeReceived:
	confirm, err := ClientConfirmation(username, password, clientPriv, clientPub, serverPub, serverParity, salt)
	if err != nil {
		return fmt.Errorf("compute EC-SRP5 confirmation: %w", err)
	}

	var wBuf, hBuf [2]byte
	binary.LittleEndian.PutUint16(wBuf[:], macTelnetTermWidth)
	binary.LittleEndian.PutUint16(hBuf[:], macTelnetTermHeight)

	var step2 bytes.Buffer
	step2.Write(EncodeControlPacket(macTelnetCPPassword, confirm[:]))
	step2.Write(EncodeControlPacket(macTelnetCPUsername, []byte(username)))
	step2.Write(EncodeControlPacket(macTelnetCPTermType, []byte("vt100")))
	step2.Write(EncodeControlPacket(macTelnetCPTermWidth, wBuf[:]))
	step2.Write(EncodeControlPacket(macTelnetCPTermHeight, hBuf[:]))

	// Redact control payload when logging raw hex because it carries the Password proof.
	if err := s.sendReliable(ctx, macTelnetPTypeData, step2.Bytes(), true); err != nil {
		return fmt.Errorf("send auth confirmation: %w", err)
	}

	// Wait for EndAuth or failure banner.
	endAuthReceived := false
	licenseSent := false
	for !endAuthReceived {
		if txt := s.peekTermClean(); strings.Contains(txt, "Login failed") || strings.Contains(txt, "incorrect") {
			return errors.New("MAC-Telnet authentication failed: login rejected by router")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for MAC-Telnet EndAuth: %w", ctx.Err())
		case <-s.doneCh:
			if txt := s.peekTermClean(); strings.Contains(txt, "Login failed") {
				return errors.New("MAC-Telnet authentication failed: login rejected by router")
			}
			return errors.New("MAC-Telnet session closed before EndAuth")
		case cp := <-s.ctrlCh:
			if cp.Type == macTelnetCPEndAuth {
				endAuthReceived = true
			}
		case <-s.termNotify:
		}
	}

	// Wait for the initial RouterOS prompt after login banner / first-boot prompts.
	passwordPromptCancelled := false
	enterSent := false
	for {
		clean := s.peekTermClean()
		lower := strings.ToLower(clean)
		if strings.Contains(lower, "login failed") {
			return errors.New("MAC-Telnet authentication failed: login rejected by router")
		}
		if !licenseSent && strings.Contains(lower, "software license") {
			licenseSent = true
			_ = s.sendReliable(ctx, macTelnetPTypeData, []byte("n"), false)
		}
		if !passwordPromptCancelled && strings.Contains(lower, "new password") {
			passwordPromptCancelled = true
			_ = s.sendReliable(ctx, macTelnetPTypeData, []byte{0x03}, false)
		}
		if !enterSent && (strings.Contains(lower, `press "enter"`) || strings.Contains(lower, "press enter")) {
			enterSent = true
			_ = s.sendReliable(ctx, macTelnetPTypeData, []byte("\r"), false)
		}
		if rosPromptRegexp.MatchString(clean) {
			s.drainTerm()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for initial RouterOS prompt: %w (last output=%s)", ctx.Err(), sanitizePromptTail(clean))
		case <-s.doneCh:
			return fmt.Errorf("MAC-Telnet session closed before initial prompt (last output=%s)", sanitizePromptTail(clean))
		case <-s.termNotify:
		}
	}
}

func sanitizePromptTail(clean string) string {
	const maxLen = 300
	if len(clean) > maxLen {
		clean = clean[len(clean)-maxLen:]
	}
	return fmt.Sprintf("%q", clean)
}

func cleanTerminalText(raw string) string {
	stripped := ansiEscapeRegexp.ReplaceAllString(raw, "")
	stripped = strings.ReplaceAll(stripped, "\r\n", "\n")
	stripped = strings.ReplaceAll(stripped, "\r", "\n")
	return stripped
}

func (s *MACTelnetSession) peekTermClean() string {
	s.mu.Lock()
	raw := s.termBuf.String()
	s.mu.Unlock()
	return cleanTerminalText(raw)
}

func (s *MACTelnetSession) drainTerm() string {
	s.mu.Lock()
	raw := s.termBuf.String()
	s.termBuf.Reset()
	s.mu.Unlock()
	return cleanTerminalText(raw)
}

// Exec executes a RouterOS CLI script line-by-line over the authenticated
// MAC-Telnet session and returns the combined terminal output.
func (s *MACTelnetSession) Exec(ctx context.Context, script string) (string, error) {
	var totalOut strings.Builder
	lines := strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		s.drainTerm()
		if err := s.sendReliable(ctx, macTelnetPTypeData, []byte(line+"\r"), false); err != nil {
			return totalOut.String(), err
		}
		out, err := s.readUntilPrompt(ctx)
		totalOut.WriteString(out)
		if err != nil {
			return totalOut.String(), err
		}
	}
	return totalOut.String(), nil
}

func (s *MACTelnetSession) readUntilPrompt(ctx context.Context) (string, error) {
	for {
		clean := s.peekTermClean()
		if rosPromptRegexp.MatchString(clean) {
			return s.drainTerm(), nil
		}
		select {
		case <-ctx.Done():
			return s.drainTerm(), fmt.Errorf("waiting for RouterOS prompt: %w (last output=%s)", ctx.Err(), sanitizePromptTail(clean))
		case <-s.doneCh:
			return s.drainTerm(), errors.New("MAC-Telnet session closed while executing command")
		case <-s.termNotify:
		}
	}
}

// Shell attaches an interactive reader/writer stream to the MAC-Telnet terminal.
func (s *MACTelnetSession) Shell(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	errCh := make(chan error, 2)

	// Output pump.
	go func() {
		for {
			if out := s.drainTerm(); out != "" {
				if _, err := io.WriteString(stdout, out); err != nil {
					errCh <- err
					return
				}
			}
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case <-s.doneCh:
				if out := s.drainTerm(); out != "" {
					_, _ = io.WriteString(stdout, out)
				}
				errCh <- nil
				return
			case <-s.termNotify:
			}
		}
	}()

	// Input pump.
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				if sendErr := s.sendReliable(ctx, macTelnetPTypeData, buf[:n], false); sendErr != nil {
					errCh <- sendErr
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case <-s.doneCh:
		return nil
	case err := <-errCh:
		return err
	}
}

// Close sends EndSession and closes the underlying UDP socket.
func (s *MACTelnetSession) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		alreadyRemoteClosed := s.remoteClosed
		rx := s.rxCounter
		s.closed = true
		s.mu.Unlock()

		if !alreadyRemoteClosed {
			endFrame := EncodeMACTelnetPacket(true, macTelnetPTypeEndSession, s.localMAC, s.targetMAC, s.sesKey, macTelnetClientType, rx, nil)
			_, _ = s.conn.WriteTo(endFrame, s.remoteAddr)
		}
		close(s.doneCh)
		closeErr = s.conn.Close()
	})
	return closeErr
}

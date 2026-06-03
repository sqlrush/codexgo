package networkproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// SOCKS5 protocol constants (RFC 1928).
const (
	socksVersion5 = 0x05

	socksAuthNone         = 0x00
	socksAuthNoAcceptable = 0xFF

	socksCmdConnect      = 0x01
	socksCmdUDPAssociate = 0x03

	socksAtypIPv4   = 0x01
	socksAtypDomain = 0x03
	socksAtypIPv6   = 0x04

	socksRepSucceeded            = 0x00
	socksRepGeneralFailure       = 0x01
	socksRepConnectionNotAllowed = 0x02
	socksRepHostUnreachable      = 0x04
	socksRepCommandNotSupported  = 0x07
)

// socks5Server is the loopback SOCKS5 proxy. It enforces policy on TCP CONNECT
// and (best-effort) UDP ASSOCIATE inspection.
type socks5Server struct {
	state     *NetworkProxyState
	decider   NetworkPolicyDecider
	enableUDP bool
	dialer    *net.Dialer
}

// runSocks5 serves the SOCKS5 proxy on the listener until ctx is cancelled.
func runSocks5(ctx context.Context, state *NetworkProxyState, listener net.Listener, decider NetworkPolicyDecider, enableUDP bool) error {
	srv := &socks5Server{
		state:     state,
		decider:   decider,
		enableUDP: enableUDP,
		dialer:    &net.Dialer{Timeout: 30 * time.Second},
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || isClosedErr(err) {
				return nil
			}
			return fmt.Errorf("accept SOCKS5 connection: %w", err)
		}
		go srv.handleConn(ctx, conn)
	}
}

func (s *socks5Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	if err := s.handshake(conn); err != nil {
		return
	}
	if err := s.handleRequest(ctx, conn); err != nil {
		return
	}
}

// handshake performs the SOCKS5 method-negotiation, accepting only no-auth.
func (s *socks5Server) handshake(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return fmt.Errorf("read socks greeting: %w", err)
	}
	if header[0] != socksVersion5 {
		return fmt.Errorf("unsupported socks version %d", header[0])
	}
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("read socks methods: %w", err)
	}
	for _, m := range methods {
		if m == socksAuthNone {
			_, err := conn.Write([]byte{socksVersion5, socksAuthNone})
			return err
		}
	}
	_, _ = conn.Write([]byte{socksVersion5, socksAuthNoAcceptable})
	return fmt.Errorf("no acceptable socks auth method")
}

// socksAddr is a parsed SOCKS5 destination address.
type socksAddr struct {
	atyp  byte
	host  string
	port  uint16
	rawIP netip.Addr
	hasIP bool
}

func (s *socks5Server) handleRequest(ctx context.Context, conn net.Conn) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return fmt.Errorf("read socks request: %w", err)
	}
	if header[0] != socksVersion5 {
		return fmt.Errorf("unsupported socks version %d", header[0])
	}
	cmd := header[1]
	atyp := header[3]
	dst, err := readSocksAddr(conn, atyp)
	if err != nil {
		s.writeReply(conn, socksRepGeneralFailure, netip.AddrPort{})
		return err
	}

	client := conn.RemoteAddr().String()
	host := NormalizeHost(dst.host)
	if host == "" {
		s.writeReply(conn, socksRepGeneralFailure, netip.AddrPort{})
		return fmt.Errorf("invalid host")
	}

	switch cmd {
	case socksCmdConnect:
		return s.handleTCPConnect(ctx, conn, host, dst.port, client)
	case socksCmdUDPAssociate:
		if !s.enableUDP {
			s.writeReply(conn, socksRepCommandNotSupported, netip.AddrPort{})
			return fmt.Errorf("udp associate disabled")
		}
		return s.handleUDPAssociate(ctx, conn, host, dst.port, client)
	default:
		s.writeReply(conn, socksRepCommandNotSupported, netip.AddrPort{})
		return fmt.Errorf("unsupported socks command %d", cmd)
	}
}

func (s *socks5Server) handleTCPConnect(ctx context.Context, conn net.Conn, host string, port uint16, client string) error {
	if blocked := s.checkTCPPolicy(ctx, host, port, client); blocked != "" {
		s.writeReply(conn, socksRepConnectionNotAllowed, netip.AddrPort{})
		return fmt.Errorf("socks blocked: %s", blocked)
	}

	address := net.JoinHostPort(host, strconv.Itoa(int(port)))
	upstream, err := s.dialChecked(ctx, host, port)
	if err != nil {
		s.writeReply(conn, socksRepHostUnreachable, netip.AddrPort{})
		return fmt.Errorf("dial %s: %w", address, err)
	}
	defer upstream.Close()

	bound := netip.AddrPort{}
	if tcpAddr, ok := upstream.LocalAddr().(*net.TCPAddr); ok {
		if a, ok := netip.AddrFromSlice(tcpAddr.IP); ok {
			bound = netip.AddrPortFrom(a.Unmap(), uint16(tcpAddr.Port))
		}
	}
	if err := s.writeReply(conn, socksRepSucceeded, bound); err != nil {
		return err
	}

	tunnel(conn, upstream)
	return nil
}

// checkTCPPolicy runs the full TCP policy chain (enabled, mode, mitm-hooks,
// host policy). Returns the block reason, or "" when allowed.
func (s *socks5Server) checkTCPPolicy(ctx context.Context, host string, port uint16, client string) string {
	if !s.state.Enabled() {
		s.emitSocksBlock(protocol.NetworkDecisionSourceProxyState, reasonProxyDisabled, ProtocolSocks5TCP, host, port, client)
		s.recordSocksBlock(ctx, host, port, client, reasonProxyDisabled, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceProxyState, "socks5", nil)
		return reasonProxyDisabled
	}
	if s.state.NetworkMode() == NetworkModeLimited {
		s.emitSocksBlock(protocol.NetworkDecisionSourceModeGuard, reasonMethodNotAllowed, ProtocolSocks5TCP, host, port, client)
		limited := NetworkModeLimited
		s.recordSocksBlock(ctx, host, port, client, reasonMethodNotAllowed, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceModeGuard, "socks5", &limited)
		return reasonMethodNotAllowed
	}
	if s.state.HostHasMitmHooks(host) {
		s.emitSocksBlock(protocol.NetworkDecisionSourceModeGuard, reasonMitmRequired, ProtocolSocks5TCP, host, port, client)
		full := NetworkModeFull
		s.recordSocksBlock(ctx, host, port, client, reasonMitmRequired, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceModeGuard, "socks5", &full)
		return reasonMitmRequired
	}
	request := NetworkPolicyRequest{Protocol: ProtocolSocks5TCP, Host: host, Port: port, ClientAddr: client}
	decision := evaluateHostPolicy(ctx, s.state, s.decider, request)
	if decision.Kind == DecisionDeny {
		s.recordSocksBlock(ctx, host, port, client, decision.Reason, decision.Decision, decision.Source, "socks5", nil)
		return decision.Reason
	}
	return ""
}

// handleUDPAssociate sets up a UDP relay with per-datagram policy inspection.
func (s *socks5Server) handleUDPAssociate(ctx context.Context, conn net.Conn, _ string, _ uint16, client string) error {
	// Bind a UDP socket on loopback for the relay.
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		s.writeReply(conn, socksRepGeneralFailure, netip.AddrPort{})
		return fmt.Errorf("bind udp relay: %w", err)
	}
	defer udpConn.Close()

	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	bound, _ := netip.AddrFromSlice(localAddr.IP)
	if err := s.writeReply(conn, socksRepSucceeded, netip.AddrPortFrom(bound.Unmap(), uint16(localAddr.Port))); err != nil {
		return err
	}

	// Close the UDP relay when the control connection drops.
	go func() {
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
		_ = udpConn.Close()
	}()

	return s.relayUDP(ctx, udpConn, client)
}

func (s *socks5Server) relayUDP(ctx context.Context, udpConn *net.UDPConn, client string) error {
	buf := make([]byte, 64*1024)
	upstreamConns := make(map[string]*net.UDPConn)
	defer func() {
		for _, c := range upstreamConns {
			_ = c.Close()
		}
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		n, clientUDPAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || isClosedErr(err) {
				return nil
			}
			return fmt.Errorf("read udp relay: %w", err)
		}
		host, port, payload, ok := parseSocksUDPDatagram(buf[:n])
		if !ok {
			continue
		}
		host = NormalizeHost(host)
		if host == "" {
			continue
		}
		if !s.checkUDPPolicy(ctx, host, port, client) {
			continue
		}
		// Forward to upstream and relay the response back to the client.
		target := net.JoinHostPort(host, strconv.Itoa(int(port)))
		raddr, err := net.ResolveUDPAddr("udp", target)
		if err != nil {
			continue
		}
		upstream, err := net.DialUDP("udp", nil, raddr)
		if err != nil {
			continue
		}
		if _, err := upstream.Write(payload); err != nil {
			upstream.Close()
			continue
		}
		go relayUDPResponse(udpConn, upstream, clientUDPAddr, host, port)
	}
}

func relayUDPResponse(client *net.UDPConn, upstream *net.UDPConn, clientAddr *net.UDPAddr, host string, port uint16) {
	defer upstream.Close()
	_ = upstream.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 64*1024)
	n, err := upstream.Read(buf)
	if err != nil {
		return
	}
	datagram := buildSocksUDPDatagram(host, port, buf[:n])
	_, _ = client.WriteToUDP(datagram, clientAddr)
}

// checkUDPPolicy runs the UDP policy chain. Returns true when allowed.
func (s *socks5Server) checkUDPPolicy(ctx context.Context, host string, port uint16, client string) bool {
	if !s.state.Enabled() {
		s.emitSocksBlock(protocol.NetworkDecisionSourceProxyState, reasonProxyDisabled, ProtocolSocks5UDP, host, port, client)
		s.recordSocksBlock(ctx, host, port, client, reasonProxyDisabled, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceProxyState, "socks5-udp", nil)
		return false
	}
	if s.state.NetworkMode() == NetworkModeLimited {
		s.emitSocksBlock(protocol.NetworkDecisionSourceModeGuard, reasonMethodNotAllowed, ProtocolSocks5UDP, host, port, client)
		limited := NetworkModeLimited
		s.recordSocksBlock(ctx, host, port, client, reasonMethodNotAllowed, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceModeGuard, "socks5-udp", &limited)
		return false
	}
	request := NetworkPolicyRequest{Protocol: ProtocolSocks5UDP, Host: host, Port: port, ClientAddr: client}
	decision := evaluateHostPolicy(ctx, s.state, s.decider, request)
	if decision.Kind == DecisionDeny {
		s.recordSocksBlock(ctx, host, port, client, decision.Reason, decision.Decision, decision.Source, "socks5-udp", nil)
		return false
	}
	return true
}

func (s *socks5Server) dialChecked(ctx context.Context, host string, port uint16) (net.Conn, error) {
	address := net.JoinHostPort(host, strconv.Itoa(int(port)))
	if !s.state.AllowLocalBinding() {
		if addr, err := netip.ParseAddr(host); err == nil {
			if isNonPublicIP(addr) {
				return nil, fmt.Errorf("network target rejected by policy")
			}
		} else {
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}
			for _, ip := range ips {
				if isNonPublicIP(ip) {
					return nil, fmt.Errorf("network target rejected by policy")
				}
			}
		}
	}
	conn, err := s.dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", address, err)
	}
	return conn, nil
}

func (s *socks5Server) emitSocksBlock(source protocol.NetworkDecisionSource, reason string, proto NetworkProtocol, host string, port uint16, client string) {
	emitBlockDecisionAuditEvent(s.state.auditSink, s.state.metadata, blockDecisionAuditArgs{
		source: source, reason: reason, protocol: proto,
		serverAddress: host, serverPort: port,
		clientAddr: client, hasClient: client != "",
	})
}

func (s *socks5Server) recordSocksBlock(ctx context.Context, host string, port uint16, client, reason string, decision protocol.NetworkPolicyDecision, source protocol.NetworkDecisionSource, proto string, mode *NetworkMode) {
	d := string(decision)
	sr := string(source)
	p := port
	s.state.RecordBlocked(ctx, newBlockedRequest(BlockedRequestArgs{
		Host: host, Reason: reason, Client: optStr(client), Mode: mode,
		Protocol: proto, Decision: &d, Source: &sr, Port: &p,
	}))
}

// writeReply writes a SOCKS5 reply with the given status and bound address.
func (s *socks5Server) writeReply(conn net.Conn, rep byte, bound netip.AddrPort) error {
	reply := []byte{socksVersion5, rep, 0x00}
	addr := bound.Addr()
	switch {
	case addr.Is4():
		b := addr.As4()
		reply = append(reply, socksAtypIPv4)
		reply = append(reply, b[:]...)
	case addr.Is6():
		b := addr.As16()
		reply = append(reply, socksAtypIPv6)
		reply = append(reply, b[:]...)
	default:
		reply = append(reply, socksAtypIPv4, 0, 0, 0, 0)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, bound.Port())
	reply = append(reply, portBytes...)
	_, err := conn.Write(reply)
	return err
}

// readSocksAddr reads a SOCKS5 address of the given type from the reader.
func readSocksAddr(r io.Reader, atyp byte) (socksAddr, error) {
	switch atyp {
	case socksAtypIPv4:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return socksAddr{}, fmt.Errorf("read ipv4 addr: %w", err)
		}
		ip := netip.AddrFrom4([4]byte{buf[0], buf[1], buf[2], buf[3]})
		return socksAddr{atyp: atyp, host: ip.String(), port: binary.BigEndian.Uint16(buf[4:]), rawIP: ip, hasIP: true}, nil
	case socksAtypIPv6:
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return socksAddr{}, fmt.Errorf("read ipv6 addr: %w", err)
		}
		var b [16]byte
		copy(b[:], buf[:16])
		ip := netip.AddrFrom16(b)
		return socksAddr{atyp: atyp, host: ip.String(), port: binary.BigEndian.Uint16(buf[16:]), rawIP: ip, hasIP: true}, nil
	case socksAtypDomain:
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(r, lenByte); err != nil {
			return socksAddr{}, fmt.Errorf("read domain length: %w", err)
		}
		buf := make([]byte, int(lenByte[0])+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return socksAddr{}, fmt.Errorf("read domain addr: %w", err)
		}
		host := string(buf[:lenByte[0]])
		return socksAddr{atyp: atyp, host: host, port: binary.BigEndian.Uint16(buf[lenByte[0]:])}, nil
	default:
		return socksAddr{}, fmt.Errorf("unsupported socks address type %d", atyp)
	}
}

// parseSocksUDPDatagram parses a SOCKS5 UDP request datagram (RFC 1928 §7).
func parseSocksUDPDatagram(data []byte) (host string, port uint16, payload []byte, ok bool) {
	if len(data) < 4 {
		return "", 0, nil, false
	}
	// data[0:2] = RSV, data[2] = FRAG, data[3] = ATYP
	if data[2] != 0x00 {
		return "", 0, nil, false // fragmentation not supported
	}
	atyp := data[3]
	offset := 4
	switch atyp {
	case socksAtypIPv4:
		if len(data) < offset+4+2 {
			return "", 0, nil, false
		}
		ip := netip.AddrFrom4([4]byte{data[offset], data[offset+1], data[offset+2], data[offset+3]})
		offset += 4
		host = ip.String()
	case socksAtypIPv6:
		if len(data) < offset+16+2 {
			return "", 0, nil, false
		}
		var b [16]byte
		copy(b[:], data[offset:offset+16])
		offset += 16
		host = netip.AddrFrom16(b).String()
	case socksAtypDomain:
		if len(data) < offset+1 {
			return "", 0, nil, false
		}
		dlen := int(data[offset])
		offset++
		if len(data) < offset+dlen+2 {
			return "", 0, nil, false
		}
		host = string(data[offset : offset+dlen])
		offset += dlen
	default:
		return "", 0, nil, false
	}
	port = binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2
	return host, port, data[offset:], true
}

// buildSocksUDPDatagram builds a SOCKS5 UDP response datagram.
func buildSocksUDPDatagram(host string, port uint16, payload []byte) []byte {
	out := []byte{0x00, 0x00, 0x00}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Is4() {
			b := addr.As4()
			out = append(out, socksAtypIPv4)
			out = append(out, b[:]...)
		} else {
			b := addr.As16()
			out = append(out, socksAtypIPv6)
			out = append(out, b[:]...)
		}
	} else {
		out = append(out, socksAtypDomain, byte(len(host)))
		out = append(out, host...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	out = append(out, portBytes...)
	out = append(out, payload...)
	return out
}

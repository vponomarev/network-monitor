//go:build linux
// +build linux

package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// TracerouteConfig holds traceroute configuration
type TracerouteConfig struct {
	MaxHops      int           `json:"max_hops"`
	Timeout      time.Duration `json:"timeout"`
	ProbesPerHop int           `json:"probes_per_hop"`
	StartTTL     int           `json:"start_ttl"`
	Protocol     string        `json:"protocol"` // icmp, udp, tcp
	DstPort      int           `json:"dst_port"`
	SrcPort      int           `json:"src_port"` // Source port for TCP traceroute
	TCPFlags     string        `json:"tcp_flags"` // TCP flags for probes (SYN, ACK, FIN)
}

// HopResult represents a single hop result from traceroute
type HopResult struct {
	TTL       int           `json:"ttl"`
	IP        string        `json:"ip,omitempty"`
	Hostname  string        `json:"hostname,omitempty"`
	RTT       time.Duration `json:"rtt,omitempty"`
	Lost      bool          `json:"lost"`
	Timeout   bool          `json:"timeout"`
	ProbeSent time.Time     `json:"probe_sent"`
}

// TracerouteResult represents the result of a traceroute
type TracerouteResult struct {
	Destination string        `json:"destination"`
	Hops        []HopResult   `json:"hops"`
	Completed   bool          `json:"completed"`
	Duration    time.Duration `json:"duration"`
}

// DefaultTracerouteConfig returns default traceroute configuration
func DefaultTracerouteConfig() *TracerouteConfig {
	return &TracerouteConfig{
		MaxHops:      30,
		Timeout:      3 * time.Second,
		ProbesPerHop: 3,
		StartTTL:     1,
		Protocol:     "icmp",
		DstPort:      33434,
		SrcPort:      0, // Auto-select
		TCPFlags:     "S", // SYN flag for TCP traceroute
	}
}

// PacketTracerouter performs network traceroute using raw packets
type PacketTracerouter interface {
	Trace(ctx context.Context, dstIP string) (*TracerouteResult, error)
}

// ICMPPacketTracerouter implements traceroute using ICMP Echo Request
type ICMPPacketTracerouter struct {
	config *TracerouteConfig
	logger *zap.Logger
	srcIP  net.IP
}

// NewICMPTracerouter creates a new ICMP-based tracerouter
func NewICMPTracerouter(config *TracerouteConfig, logger *zap.Logger) (*ICMPPacketTracerouter, error) {
	if config == nil {
		config = DefaultTracerouteConfig()
	}

	// Get source IP
	srcIP, err := getOutboundIP()
	if err != nil {
		return nil, fmt.Errorf("getting source IP: %w", err)
	}

	return &ICMPPacketTracerouter{
		config: config,
		logger: logger.Named("traceroute"),
		srcIP:  srcIP,
	}, nil
}

// Trace performs traceroute to destination
func (t *ICMPPacketTracerouter) Trace(ctx context.Context, dstIP string) (*TracerouteResult, error) {
	start := time.Now()
	t.logger.Debug("Starting ICMP traceroute", zap.String("dst", dstIP))

	result := &TracerouteResult{
		Destination: dstIP,
		Hops:        make([]HopResult, 0, t.config.MaxHops),
		Completed:   false,
	}

	// Parse destination
	dst := net.ParseIP(dstIP)
	if dst == nil {
		return nil, fmt.Errorf("invalid destination IP: %s", dstIP)
	}

	// Create ICMP connection (raw socket)
	conn, err := createICMPConnection()
	if err != nil {
		return nil, fmt.Errorf("creating ICMP connection: %w", err)
	}
	defer conn.Close()

	// Trace each hop
	for ttl := t.config.StartTTL; ttl <= t.config.MaxHops; ttl++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		hop := t.traceHop(ctx, conn, dst, ttl)
		result.Hops = append(result.Hops, hop)

		// Check if we reached the destination
		if !hop.Lost && !hop.Timeout && hop.IP == dstIP {
			result.Completed = true
			break
		}
	}

	result.Duration = time.Since(start)
	t.logger.Debug("Traceroute completed",
		zap.String("dst", dstIP),
		zap.Int("hops", len(result.Hops)),
		zap.Bool("completed", result.Completed),
		zap.Duration("duration", result.Duration))

	return result, nil
}

// traceHop traces a single hop
func (t *ICMPPacketTracerouter) traceHop(ctx context.Context, conn *icmpConn, dst net.IP, ttl int) HopResult {
	hop := HopResult{
		TTL:       ttl,
		Lost:      true,
		Timeout:   true,
		ProbeSent: time.Now(),
	}

	// Set TTL on socket
	if err := conn.SetTTL(ttl); err != nil {
		t.logger.Error("Failed to set TTL", zap.Int("ttl", ttl), zap.Error(err))
		return hop
	}

	// Send probes
	var rtts []time.Duration
	for i := 0; i < t.config.ProbesPerHop; i++ {
		select {
		case <-ctx.Done():
			return hop
		default:
		}

		rtt, ip, err := t.sendProbe(conn, dst)
		if err == nil && ip != nil {
			hop.Lost = false
			hop.Timeout = false
			hop.IP = ip.String()
			rtts = append(rtts, rtt)

			// Try reverse DNS lookup
			if names, err := net.LookupAddr(ip.String()); err == nil && len(names) > 0 {
				hop.Hostname = names[0]
			}
		}
	}

	// Calculate average RTT
	if len(rtts) > 0 {
		var total time.Duration
		for _, rtt := range rtts {
			total += rtt
		}
		hop.RTT = total / time.Duration(len(rtts))
	}

	return hop
}

// sendProbe sends a single ICMP probe and waits for response
func (t *ICMPPacketTracerouter) sendProbe(conn *icmpConn, dst net.IP) (time.Duration, net.IP, error) {
	start := time.Now()

	// Create ICMP Echo Request
	seq := time.Now().Nanosecond() & 0xFFFF
	icmpData := createICMPEchoRequest(seq)

	// Send
	if err := conn.SendTo(icmpData, dst); err != nil {
		return 0, nil, fmt.Errorf("sending probe: %w", err)
	}

	// Wait for response with timeout
	conn.SetReadDeadline(time.Now().Add(t.config.Timeout))
	response, fromIP, err := conn.RecvFrom()
	if err != nil {
		return 0, nil, err
	}

	rtt := time.Since(start)

	// Parse ICMP response
	if isICMPTimeExceeded(response) || isICMPEchoReply(response) {
		return rtt, fromIP, nil
	}

	return 0, nil, fmt.Errorf("unexpected ICMP type")
}

// createICMPEchoRequest creates an ICMP Echo Request packet
func createICMPEchoRequest(seq int) []byte {
	const (
		ICMP_ECHO_REQUEST = 8
		ICMP_ECHO_REPLY   = 0
	)

	// ICMP header: Type(1) + Code(1) + Checksum(2) + ID(2) + Seq(2) + Data(48)
	id := os.Getpid() & 0xFFFF
	data := make([]byte, 56)

	// Fill data with pattern
	for i := range data {
		data[i] = byte(i)
	}

	// Build packet
	pkt := make([]byte, 64)
	pkt[0] = ICMP_ECHO_REQUEST // Type
	pkt[1] = 0                  // Code
	binary.BigEndian.PutUint16(pkt[2:4], 0) // Checksum (filled later)
	binary.BigEndian.PutUint16(pkt[4:6], uint16(id))
	binary.BigEndian.PutUint16(pkt[6:8], uint16(seq))
	copy(pkt[8:], data)

	// Calculate checksum
	checksum := calculateICMPChecksum(pkt)
	binary.BigEndian.PutUint16(pkt[2:4], checksum)

	return pkt
}

// calculateICMPChecksum calculates ICMP checksum
func calculateICMPChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

// isICMPTimeExceeded checks if packet is ICMP Time Exceeded
func isICMPTimeExceeded(data []byte) bool {
	return len(data) >= 1 && data[0] == 11 // Type 11: Time Exceeded
}

// isICMPEchoReply checks if packet is ICMP Echo Reply
func isICMPEchoReply(data []byte) bool {
	return len(data) >= 1 && data[0] == 0 // Type 0: Echo Reply
}

// icmpConn represents an ICMP connection
type icmpConn struct {
	fd int
}

// createICMPConnection creates a raw ICMP socket
func createICMPConnection() (*icmpConn, error) {
	// Create raw socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, fmt.Errorf("creating raw socket: %w", err)
	}

	// Set non-blocking
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("setting non-blocking: %w", err)
	}

	return &icmpConn{fd: fd}, nil
}

// SetTTL sets the TTL for outgoing packets
func (c *icmpConn) SetTTL(ttl int) error {
	return syscall.SetsockoptInt(c.fd, syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
}

// SetReadDeadline sets the read timeout
func (c *icmpConn) SetReadDeadline(deadline time.Time) error {
	timeout := time.Until(deadline)
	if timeout < 0 {
		timeout = 0
	}
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}

// SendTo sends data to destination
func (c *icmpConn) SendTo(data []byte, dst net.IP) error {
	dst4 := dst.To4()
	if dst4 == nil {
		return fmt.Errorf("IPv4 address required")
	}

	addr := &syscall.SockaddrInet4{
		Addr: [4]byte(dst4),
		Port: 0,
	}

	return syscall.Sendto(c.fd, data, 0, addr)
}

// RecvFrom receives data and returns source IP
func (c *icmpConn) RecvFrom() ([]byte, net.IP, error) {
	buf := make([]byte, 1500)
	n, from, err := syscall.Recvfrom(c.fd, buf, 0)
	if err != nil {
		return nil, nil, err
	}

	var srcIP net.IP
	if addr, ok := from.(*syscall.SockaddrInet4); ok {
		srcIP = net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	}

	// Skip IP header (usually 20 bytes) to get to ICMP payload
	if n > 20 {
		return buf[20:n], srcIP, nil
	}

	return buf[:n], srcIP, nil
}

// Close closes the connection
func (c *icmpConn) Close() error {
	return syscall.Close(c.fd)
}

// getOutboundIP gets the preferred outbound IP address
func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP, nil
}

// UDPPacketTracerouter implements traceroute using UDP probes
type UDPPacketTracerouter struct {
	config *TracerouteConfig
	logger *zap.Logger
	srcIP  net.IP
}

// NewUDPTracerouter creates a new UDP-based tracerouter
func NewUDPTracerouter(config *TracerouteConfig, logger *zap.Logger) (*UDPPacketTracerouter, error) {
	if config == nil {
		config = DefaultTracerouteConfig()
	}

	srcIP, err := getOutboundIP()
	if err != nil {
		return nil, fmt.Errorf("getting source IP: %w", err)
	}

	return &UDPPacketTracerouter{
		config: config,
		logger: logger.Named("traceroute-udp"),
		srcIP:  srcIP,
	}, nil
}

// Trace performs UDP traceroute
func (t *UDPPacketTracerouter) Trace(ctx context.Context, dstIP string) (*TracerouteResult, error) {
	start := time.Now()
	t.logger.Debug("Starting UDP traceroute", zap.String("dst", dstIP))

	result := &TracerouteResult{
		Destination: dstIP,
		Hops:        make([]HopResult, 0, t.config.MaxHops),
		Completed:   false,
	}

	dst := net.ParseIP(dstIP)
	if dst == nil {
		return nil, fmt.Errorf("invalid destination IP: %s", dstIP)
	}

	// Create UDP connection
	conn, err := createUDPConnection()
	if err != nil {
		return nil, fmt.Errorf("creating UDP connection: %w", err)
	}
	defer conn.Close()

	// Trace each hop
	for ttl := t.config.StartTTL; ttl <= t.config.MaxHops; ttl++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		hop := t.traceHopUDP(ctx, conn, dst, ttl)
		result.Hops = append(result.Hops, hop)

		// Check if we reached destination (port unreachable = reached)
		if !hop.Lost && !hop.Timeout {
			result.Completed = true
			break
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (t *UDPPacketTracerouter) traceHopUDP(ctx context.Context, conn *udpConn, dst net.IP, ttl int) HopResult {
	hop := HopResult{
		TTL:       ttl,
		Lost:      true,
		Timeout:   true,
		ProbeSent: time.Now(),
	}

	if err := conn.SetTTL(ttl); err != nil {
		return hop
	}

	var rtts []time.Duration
	for i := 0; i < t.config.ProbesPerHop; i++ {
		select {
		case <-ctx.Done():
			return hop
		default:
		}

		rtt, ip, err := t.sendUDPProbe(conn, dst, t.config.DstPort+i)
		if err == nil && ip != nil {
			hop.Lost = false
			hop.Timeout = false
			hop.IP = ip.String()
			rtts = append(rtts, rtt)

			if names, err := net.LookupAddr(ip.String()); err == nil && len(names) > 0 {
				hop.Hostname = names[0]
			}
		}
	}

	if len(rtts) > 0 {
		var total time.Duration
		for _, rtt := range rtts {
			total += rtt
		}
		hop.RTT = total / time.Duration(len(rtts))
	}

	return hop
}

func (t *UDPPacketTracerouter) sendUDPProbe(conn *udpConn, dst net.IP, port int) (time.Duration, net.IP, error) {
	start := time.Now()

	data := []byte("traceroute")
	if err := conn.SendTo(data, dst, port); err != nil {
		return 0, nil, err
	}

	conn.SetReadDeadline(time.Now().Add(t.config.Timeout))
	response, fromIP, err := conn.RecvFrom()
	if err != nil {
		return 0, nil, err
	}

	rtt := time.Since(start)

	// ICMP Port Unreachable (Type 3, Code 3) means we reached destination
	if isICMPPortUnreachable(response) {
		return rtt, fromIP, nil
	}

	// ICMP Time Exceeded means intermediate hop
	if isICMPTimeExceeded(response) {
		return rtt, fromIP, nil
	}

	return 0, nil, fmt.Errorf("unexpected response")
}

func isICMPPortUnreachable(data []byte) bool {
	return len(data) >= 2 && data[0] == 3 && data[1] == 3
}

// udpConn represents a UDP connection for traceroute
type udpConn struct {
	fd int
}

func createUDPConnection() (*udpConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, err
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}

	return &udpConn{fd: fd}, nil
}

func (c *udpConn) SetTTL(ttl int) error {
	return syscall.SetsockoptInt(c.fd, syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
}

func (c *udpConn) SetReadDeadline(deadline time.Time) error {
	timeout := time.Until(deadline)
	if timeout < 0 {
		timeout = 0
	}
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}

func (c *udpConn) SendTo(data []byte, dst net.IP, port int) error {
	dst4 := dst.To4()
	if dst4 == nil {
		return fmt.Errorf("IPv4 address required")
	}

	addr := &syscall.SockaddrInet4{
		Addr: [4]byte(dst4),
		Port: port,
	}

	return syscall.Sendto(c.fd, data, 0, addr)
}

func (c *udpConn) RecvFrom() ([]byte, net.IP, error) {
	buf := make([]byte, 1500)
	n, from, err := syscall.Recvfrom(c.fd, buf, 0)
	if err != nil {
		return nil, nil, err
	}

	var srcIP net.IP
	if addr, ok := from.(*syscall.SockaddrInet4); ok {
		srcIP = net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	}

	// Skip IP header to get ICMP payload
	if n > 20 {
		return buf[20:n], srcIP, nil
	}

	return buf[:n], srcIP, nil
}

func (c *udpConn) Close() error {
	return syscall.Close(c.fd)
}

// TracerouteFactory creates appropriate tracerouter based on config
type TracerouteFactory struct {
	config *TracerouteConfig
	logger *zap.Logger
}

// NewTracerouteFactory creates a new traceroute factory
func NewTracerouteFactory(config *TracerouteConfig, logger *zap.Logger) *TracerouteFactory {
	if config == nil {
		config = DefaultTracerouteConfig()
	}
	return &TracerouteFactory{
		config: config,
		logger: logger,
	}
}

// Create creates a tracerouter based on protocol
func (f *TracerouteFactory) Create(protocol string) (PacketTracerouter, error) {
	switch protocol {
	case "udp":
		return NewUDPTracerouter(f.config, f.logger)
	case "tcp":
		return NewTCPTracerouter(f.config, f.logger)
	case "icmp", "":
		return NewICMPTracerouter(f.config, f.logger)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s (use icmp, udp, or tcp)", protocol)
	}
}

// TraceroutePool manages multiple concurrent traceroutes
type TraceroutePool struct {
	factory     *TracerouteFactory
	maxConcurrent int
	semaphore   chan struct{}
}

// NewTraceroutePool creates a new traceroute pool
func NewTraceroutePool(factory *TracerouteFactory, maxConcurrent int) *TraceroutePool {
	return &TraceroutePool{
		factory:     factory,
		maxConcurrent: maxConcurrent,
		semaphore:   make(chan struct{}, maxConcurrent),
	}
}

// Trace performs traceroute with concurrency control
func (p *TraceroutePool) Trace(ctx context.Context, dstIP string) (*TracerouteResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	}

	tracerouter, err := p.factory.Create("icmp")
	if err != nil {
		return nil, err
	}

	return tracerouter.Trace(ctx, dstIP)
}

// TraceBatch performs multiple traceroutes concurrently
func (p *TraceroutePool) TraceBatch(ctx context.Context, dstIPs []string) ([]*TracerouteResult, error) {
	var wg sync.WaitGroup
	results := make([]*TracerouteResult, len(dstIPs))
	errors := make([]error, len(dstIPs))

	for i, dstIP := range dstIPs {
		wg.Add(1)
		go func(idx int, ip string) {
			defer wg.Done()
			result, err := p.Trace(ctx, ip)
			results[idx] = result
			errors[idx] = err
		}(i, dstIP)
	}

	wg.Wait()

	// Check for errors
	for _, err := range errors {
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// TCPPacketTracerouter implements traceroute using TCP SYN probes
type TCPPacketTracerouter struct {
	config *TracerouteConfig
	logger *zap.Logger
	srcIP  net.IP
}

// NewTCPTracerouter creates a new TCP-based tracerouter
func NewTCPTracerouter(config *TracerouteConfig, logger *zap.Logger) (*TCPPacketTracerouter, error) {
	if config == nil {
		config = DefaultTracerouteConfig()
	}

	srcIP, err := getOutboundIP()
	if err != nil {
		return nil, fmt.Errorf("getting source IP: %w", err)
	}

	return &TCPPacketTracerouter{
		config: config,
		logger: logger.Named("traceroute-tcp"),
		srcIP:  srcIP,
	}, nil
}

// Trace performs TCP traceroute to destination
func (t *TCPPacketTracerouter) Trace(ctx context.Context, dstIP string) (*TracerouteResult, error) {
	start := time.Now()
	t.logger.Debug("Starting TCP traceroute", zap.String("dst", dstIP))

	result := &TracerouteResult{
		Destination: dstIP,
		Hops:        make([]HopResult, 0, t.config.MaxHops),
		Completed:   false,
	}

	dst := net.ParseIP(dstIP)
	if dst == nil {
		return nil, fmt.Errorf("invalid destination IP: %s", dstIP)
	}

	// Create raw socket for TCP
	conn, err := createTCPConnection()
	if err != nil {
		return nil, fmt.Errorf("creating TCP connection: %w", err)
	}
	defer conn.Close()

	// Trace each hop
	for ttl := t.config.StartTTL; ttl <= t.config.MaxHops; ttl++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		hop := t.traceHopTCP(ctx, conn, dst, ttl)
		result.Hops = append(result.Hops, hop)

		// Check if we reached the destination (SYN-ACK or RST received)
		if !hop.Lost && !hop.Timeout && hop.IP == dstIP {
			result.Completed = true
			break
		}
	}

	result.Duration = time.Since(start)
	t.logger.Debug("TCP traceroute completed",
		zap.String("dst", dstIP),
		zap.Int("hops", len(result.Hops)),
		zap.Bool("completed", result.Completed),
		zap.Duration("duration", result.Duration))

	return result, nil
}

func (t *TCPPacketTracerouter) traceHopTCP(ctx context.Context, conn *tcpConn, dst net.IP, ttl int) HopResult {
	hop := HopResult{
		TTL:       ttl,
		Lost:      true,
		Timeout:   true,
		ProbeSent: time.Now(),
	}

	if err := conn.SetTTL(ttl); err != nil {
		t.logger.Error("Failed to set TTL", zap.Int("ttl", ttl), zap.Error(err))
		return hop
	}

	var rtts []time.Duration
	for i := 0; i < t.config.ProbesPerHop; i++ {
		select {
		case <-ctx.Done():
			return hop
		default:
		}

		port := t.config.DstPort + i
		rtt, ip, err := t.sendTCPProbe(conn, dst, port)
		if err == nil && ip != nil {
			hop.Lost = false
			hop.Timeout = false
			hop.IP = ip.String()
			rtts = append(rtts, rtt)

			if names, err := net.LookupAddr(ip.String()); err == nil && len(names) > 0 {
				hop.Hostname = names[0]
			}
		}
	}

	if len(rtts) > 0 {
		var total time.Duration
		for _, rtt := range rtts {
			total += rtt
		}
		hop.RTT = total / time.Duration(len(rtts))
	}

	return hop
}

func (t *TCPPacketTracerouter) sendTCPProbe(conn *tcpConn, dst net.IP, port int) (time.Duration, net.IP, error) {
	start := time.Now()

	// Parse TCP flags
	flags := parseTCPFlags(t.config.TCPFlags)

	// Create TCP packet
	tcpData := createTCPPacket(t.srcIP, dst, t.config.SrcPort, port, flags, 0)

	// Send
	if err := conn.SendTo(tcpData, dst, port); err != nil {
		return 0, nil, fmt.Errorf("sending TCP probe: %w", err)
	}

	// Wait for response
	conn.SetReadDeadline(time.Now().Add(t.config.Timeout))
	response, fromIP, err := conn.RecvFrom()
	if err != nil {
		return 0, nil, err
	}

	rtt := time.Since(start)

	// Check for SYN-ACK (reached destination) or RST (intermediate hop or closed port)
	if isTCPSynAck(response) || isTCPRst(response) {
		return rtt, fromIP, nil
	}

	// ICMP Time Exceeded or Port Unreachable
	if isICMPTimeExceeded(response) || isICMPPortUnreachable(response) {
		return rtt, fromIP, nil
	}

	return 0, nil, fmt.Errorf("unexpected TCP response")
}

// parseTCPFlags converts flag string to byte
func parseTCPFlags(flags string) byte {
	var result byte
	for _, f := range flags {
		switch f {
		case 'S', 's':
			result |= 0x02 // SYN
		case 'A', 'a':
			result |= 0x10 // ACK
		case 'F', 'f':
			result |= 0x01 // FIN
		case 'R', 'r':
			result |= 0x04 // RST
		case 'P', 'p':
			result |= 0x08 // PSH
		case 'U', 'u':
			result |= 0x20 // URG
		}
	}
	return result
}

// createTCPPacket creates a TCP packet with specified flags
func createTCPPacket(srcIP, dstIP net.IP, srcPort, dstPort int, flags byte, seq uint32) []byte {
	// TCP header: Src(2) + Dst(2) + Seq(4) + Ack(4) + Off(1) + Flags(1) + Win(2) + Chksum(2) + Urg(2) = 20 bytes
	tcpHeader := make([]byte, 20)

	binary.BigEndian.PutUint16(tcpHeader[0:2], uint16(srcPort))
	binary.BigEndian.PutUint16(tcpHeader[2:4], uint16(dstPort))
	binary.BigEndian.PutUint32(tcpHeader[4:8], seq)
	binary.BigEndian.PutUint32(tcpHeader[8:12], 0) // ACK number
	tcpHeader[12] = 0x50                          // Data offset (5 * 4 = 20 bytes, no options)
	tcpHeader[13] = flags                         // Flags
	binary.BigEndian.PutUint16(tcpHeader[14:16], 65535) // Window size
	// Checksum will be calculated by kernel for raw sockets
	binary.BigEndian.PutUint16(tcpHeader[18:20], 0) // Urgent pointer

	return tcpHeader
}

// isTCPSynAck checks if packet is TCP SYN-ACK
func isTCPSynAck(data []byte) bool {
	if len(data) < 14 {
		return false
	}
	flags := data[13]
	return (flags & 0x12) == 0x12 // SYN + ACK
}

// isTCPRst checks if packet is TCP RST
func isTCPRst(data []byte) bool {
	if len(data) < 14 {
		return false
	}
	flags := data[13]
	return (flags & 0x04) != 0 // RST flag
}

// tcpConn represents a TCP connection for traceroute
type tcpConn struct {
	fd int
}

func createTCPConnection() (*tcpConn, error) {
	// Create raw socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_TCP)
	if err != nil {
		return nil, fmt.Errorf("creating raw TCP socket: %w", err)
	}

	// Set non-blocking
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("setting non-blocking: %w", err)
	}

	return &tcpConn{fd: fd}, nil
}

func (c *tcpConn) SetTTL(ttl int) error {
	return syscall.SetsockoptInt(c.fd, syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
}

func (c *tcpConn) SetReadDeadline(deadline time.Time) error {
	timeout := time.Until(deadline)
	if timeout < 0 {
		timeout = 0
	}
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	return syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
}

func (c *tcpConn) SendTo(data []byte, dst net.IP, port int) error {
	dst4 := dst.To4()
	if dst4 == nil {
		return fmt.Errorf("IPv4 address required")
	}

	addr := &syscall.SockaddrInet4{
		Addr: [4]byte(dst4),
		Port: port,
	}

	return syscall.Sendto(c.fd, data, 0, addr)
}

func (c *tcpConn) RecvFrom() ([]byte, net.IP, error) {
	buf := make([]byte, 1500)
	n, from, err := syscall.Recvfrom(c.fd, buf, 0)
	if err != nil {
		return nil, nil, err
	}

	var srcIP net.IP
	if addr, ok := from.(*syscall.SockaddrInet4); ok {
		srcIP = net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	}

	// Skip IP header (usually 20 bytes) to get to TCP/ICMP payload
	if n > 20 {
		return buf[20:n], srcIP, nil
	}

	return buf[:n], srcIP, nil
}

func (c *tcpConn) Close() error {
	return syscall.Close(c.fd)
}

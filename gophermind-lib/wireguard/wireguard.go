// Package wireguard provides userspace WireGuard tunnel management for
// gophermind. It wraps golang.zx2c4.com/wireguard with gVisor's gonet
// userspace TCP/IP stack so no TUN device or sudo is required.
//
// Server: creates a WG interface, registers peers, serves HTTP over the tunnel.
// Client: establishes a tunnel to a server, routes HTTP traffic through it.
//
// All keys in the public API are hex-encoded (64 hex chars for 32-byte keys),
// matching the WireGuard uapi format.
package wireguard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const (
	// DefaultMTU is the WireGuard MTU (1420 bytes, standard for WG).
	DefaultMTU = 1420
	// DefaultListenPort is the default UDP port for the WG interface.
	DefaultListenPort = 51820
)

// ServerConfig holds the configuration for a WireGuard server interface.
type ServerConfig struct {
	// ListenPort is the UDP port to bind. 0 means use DefaultListenPort.
	ListenPort uint16
	// Address is the server's IP on the tunnel (e.g. 10.66.0.1).
	Address netip.Addr
	// PrivateKey is the 32-byte Curve25519 private key. If nil, one is generated.
	PrivateKey []byte
}

// PeerConfig is the configuration returned to a client for establishing a tunnel.
type PeerConfig struct {
	// PublicKey is the client's public key (hex, 64 chars).
	PublicKey string
	// ServerPublicKey is the server's public key (hex, 64 chars).
	ServerPublicKey string
	// Endpoint is the server's endpoint (host:port).
	Endpoint string
	// AllowedIPs is the CIDR the client should route through the tunnel.
	AllowedIPs string
	// ClientAddress is the IP assigned to the client on the tunnel.
	ClientAddress string
}

// derivePublicKey computes the Curve25519 public key from a 32-byte private key.
func derivePublicKey(priv []byte) string {
	var pub [32]byte
	var privArr [32]byte
	copy(privArr[:], priv)
	curve25519.ScalarBaseMult(&pub, &privArr)
	return hex.EncodeToString(pub[:])
}

// Server manages a userspace WireGuard interface with registered peers.
type Server struct {
	mu     sync.Mutex
	dev    *device.Device
	tnet   *netstack.Net
	tun    tunDevice
	port   uint16
	peers  map[string]*peerEntry // keyed by public key (hex)
	nextIP int
	cfg    ServerConfig
	closed bool
	pubKey string
}

type peerEntry struct {
	config PeerConfig
	added  time.Time
}

type tunDevice interface {
	Close() error
}

// NewServer creates and starts a userspace WireGuard server.
func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	if cfg.Address == (netip.Addr{}) {
		cfg.Address = netip.MustParseAddr("10.66.0.1")
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = DefaultListenPort
	}
	if len(cfg.PrivateKey) == 0 {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate private key: %w", err)
		}
		cfg.PrivateKey = key
	}

	// Create userspace TUN with gVisor TCP/IP stack.
	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{cfg.Address},
		[]netip.Addr{}, // no DNS
		DefaultMTU,
	)
	if err != nil {
		return nil, fmt.Errorf("create net TUN: %w", err)
	}

	logger := device.NewLogger(device.LogLevelError, "wg-server")
	dev := device.NewDevice(tun, conn.NewDefaultBind(), logger)

	// Configure the device via IPC. Keys are hex-encoded in the uapi.
	privateKeyHex := hex.EncodeToString(cfg.PrivateKey)
	ipc := fmt.Sprintf("private_key=%s\nlisten_port=%d\n", privateKeyHex, cfg.ListenPort)
	if err := dev.IpcSet(ipc); err != nil {
		tun.Close()
		return nil, fmt.Errorf("configure device: %w", err)
	}

	if err := dev.Up(); err != nil {
		tun.Close()
		return nil, fmt.Errorf("bring up device: %w", err)
	}

	s := &Server{
		dev:    dev,
		tnet:   tnet,
		tun:    tun,
		port:   cfg.ListenPort,
		peers:  make(map[string]*peerEntry),
		nextIP: 2, // clients start at .2
		cfg:    cfg,
		pubKey: derivePublicKey(cfg.PrivateKey),
	}

	// Shut down when context is cancelled.
	go func() {
		<-ctx.Done()
		s.Close()
	}()

	return s, nil
}

// PublicKey returns the server's public key as a hex string (64 chars).
func (s *Server) PublicKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pubKey
}

// RegisterPeer adds a new peer to the WireGuard interface and returns its config.
// clientPubKeyHex must be a 64-char hex string (32-byte public key).
func (s *Server) RegisterPeer(clientPubKeyHex string) (*PeerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.peers[clientPubKeyHex]; exists {
		return nil, fmt.Errorf("peer already registered: %s", clientPubKeyHex)
	}

	// Assign an IP to the client.
	srvIP := s.cfg.Address.As4()
	clientIP := netip.AddrFrom4([4]byte{srvIP[0], srvIP[1], srvIP[2], byte(s.nextIP)})
	s.nextIP++

	// Configure the peer via IPC.
	peerIPC := fmt.Sprintf(
		"public_key=%s\nreplace_allowed_ips=true\nallowed_ip=%s/32\npersistent_keepalive_interval=25\n",
		clientPubKeyHex,
		clientIP.String(),
	)
	if err := s.dev.IpcSet(peerIPC); err != nil {
		return nil, fmt.Errorf("configure peer: %w", err)
	}

	// Build the config to return to the client.
	cfg := &PeerConfig{
		PublicKey:       clientPubKeyHex,
		ServerPublicKey: s.pubKey,
		Endpoint:        fmt.Sprintf("127.0.0.1:%d", s.port),
		AllowedIPs:      s.cfg.Address.String() + "/32",
		ClientAddress:   clientIP.String(),
	}

	s.peers[clientPubKeyHex] = &peerEntry{
		config: *cfg,
		added:  time.Now(),
	}

	return cfg, nil
}

// RemovePeer removes a peer from the WireGuard interface.
func (s *Server) RemovePeer(clientPubKeyHex string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.peers[clientPubKeyHex]; !ok {
		return fmt.Errorf("peer not found: %s", clientPubKeyHex)
	}

	peerIPC := fmt.Sprintf("public_key=%s\nremove=true\n", clientPubKeyHex)
	if err := s.dev.IpcSet(peerIPC); err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}

	delete(s.peers, clientPubKeyHex)
	return nil
}

// PeerCount returns the number of registered peers.
func (s *Server) PeerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.peers)
}

// ListenTCP creates a TCP listener on the tunnel network.
func (s *Server) ListenTCP(port int) (net.Listener, error) {
	return s.tnet.ListenTCP(&net.TCPAddr{Port: port})
}

// Close shuts down the WireGuard server.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

// closeLocked performs the actual shutdown. Caller must hold s.mu.
func (s *Server) closeLocked() error {
	if s.closed {
		return nil
	}
	s.closed = true

	for key := range s.peers {
		peerIPC := fmt.Sprintf("public_key=%s\nremove=true\n", key)
		s.dev.IpcSet(peerIPC)
	}
	s.peers = make(map[string]*peerEntry)

	// Close the TUN first to stop the read routine, then bring the
	// device down. Closing in the reverse order races: the read
	// routine sees the closed TUN and calls device.Close() in a
	// new goroutine, which double-closes the TUN.
	err := s.tun.Close()
	s.dev.Down()
	return err
}

// ClientConfig holds the configuration for a WireGuard client tunnel.
type ClientConfig struct {
	// ServerPublicKey is the server's public key (hex, 64 chars).
	ServerPublicKey string
	// ServerEndpoint is the server's endpoint (host:port).
	ServerEndpoint string
	// PrivateKey is the client's 32-byte private key. If nil, one is generated.
	PrivateKey []byte
	// Address is the client's IP on the tunnel (e.g. 10.66.0.2).
	Address netip.Addr
	// AllowedIPs is the CIDR to route through the tunnel.
	AllowedIPs string
}

// Client is a userspace WireGuard client that routes traffic through a tunnel.
type Client struct {
	mu     sync.Mutex
	dev    *device.Device
	tnet   *netstack.Net
	tun    tunDevice
	cfg    ClientConfig
	closed bool
	pubKey string
}

// NewClient creates and starts a userspace WireGuard client tunnel.
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.Address == (netip.Addr{}) {
		cfg.Address = netip.MustParseAddr("10.66.0.2")
	}
	if len(cfg.PrivateKey) == 0 {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate private key: %w", err)
		}
		cfg.PrivateKey = key
	}

	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{cfg.Address},
		[]netip.Addr{},
		DefaultMTU,
	)
	if err != nil {
		return nil, fmt.Errorf("create net TUN: %w", err)
	}

	logger := device.NewLogger(device.LogLevelError, "wg-client")
	dev := device.NewDevice(tun, conn.NewDefaultBind(), logger)

	// Configure the device and the server peer via IPC. Keys are hex-encoded.
	privateKeyHex := hex.EncodeToString(cfg.PrivateKey)
	ipc := fmt.Sprintf(
		"private_key=%s\npublic_key=%s\nendpoint=%s\nreplace_allowed_ips=true\nallowed_ip=%s\npersistent_keepalive_interval=25\n",
		privateKeyHex,
		cfg.ServerPublicKey,
		cfg.ServerEndpoint,
		cfg.AllowedIPs,
	)
	if err := dev.IpcSet(ipc); err != nil {
		tun.Close()
		return nil, fmt.Errorf("configure device: %w", err)
	}

	if err := dev.Up(); err != nil {
		tun.Close()
		return nil, fmt.Errorf("bring up device: %w", err)
	}

	c := &Client{
		dev:  dev,
		tnet: tnet,
		tun:  tun,
		cfg:  cfg,
		pubKey: derivePublicKey(cfg.PrivateKey),
	}

	go func() {
		<-ctx.Done()
		c.Close()
	}()

	return c, nil
}

// PublicKey returns the client's public key as a hex string (64 chars).
func (c *Client) PublicKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pubKey
}

// DialTCP opens a TCP connection through the tunnel to the given address.
func (c *Client) DialTCP(addr *net.TCPAddr) (net.Conn, error) {
	return c.tnet.DialTCP(addr)
}

// Close shuts down the WireGuard client tunnel.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

// closeLocked performs the actual shutdown. Caller must hold c.mu.
func (c *Client) closeLocked() error {
	if c.closed {
		return nil
	}
	c.closed = true

	// Close the TUN first to stop the read routine, then bring the
	// device down. Closing in the reverse order races: the read
	// routine sees the closed TUN and calls device.Close() in a
	// new goroutine, which double-closes the TUN.
	err := c.tun.Close()
	c.dev.Down()
	return err
}

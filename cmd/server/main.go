package main

import (
	"crypto/tls"
	"d58-vpn/pkg/nettools"
	"d58-vpn/pkg/protocol"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/coreos/go-iptables/iptables"
	"github.com/songgao/water"
)

// ServerConfig holds the server state
type ServerConfig struct {
	TunDevice   *water.Interface
	Clients     map[string]net.Conn
	ClientsLock sync.RWMutex
	NextIP      net.IP
	ForwardAddr string
}

func main() {
	listenAddr := flag.String("listen", ":443", "Address to listen on (e.g. :443)")
	forwardAddr := flag.String("forward", "", "Next hop address (e.g. 1.2.3.4:443). If empty, acts as Exit Node.")
	certFile := flag.String("cert", "server.crt", "Path to TLS certificate")
	keyFile := flag.String("key", "server.key", "Path to TLS private key")
	flag.Parse()

	// 0. Verify Root
	if os.Geteuid() != 0 {
		log.Fatal("Server must run as root to manage TUN/NAT.")
	}

	server := &ServerConfig{
		Clients:     make(map[string]net.Conn),
		NextIP:      net.ParseIP("10.8.0.2"),
		ForwardAddr: *forwardAddr,
	}

	// 1. Setup Networking (Exit Node ONLY)
	if *forwardAddr == "" {
		log.Println("Mode: EXIT NODE (NAT Enabled)")
		log.Println("Setting up network...")
		if err := nettools.EnableIPForwarding(); err != nil {
			log.Fatalf("Failed to enable IP forwarding: %v", err)
		}

		// Create TUN interface with Server IP 10.8.0.1
		tunDev, _, err := nettools.CreateTUN("tun0", "10.8.0.1/24")
		if err != nil {
			log.Fatalf("Failed to create TUN device: %v", err)
		}
		defer tunDev.Close()
		server.TunDevice = tunDev

		// Setup NAT
		if err := setupNAT("10.8.0.0/24"); err != nil {
			log.Fatalf("Failed to setup NAT: %v", err)
		}
		defer cleanupNAT("10.8.0.0/24")

		// Start routing from TUN to Clients
		go server.routeTunToClients()
	} else {
		log.Printf("Mode: RELAY NODE (Forwarding to %s)", *forwardAddr)
	}

	// 2. Setup TLS
	cer, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		log.Fatalf("Failed to load key pair: %v", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cer}}

	// 3. Listen for connections
	listener, err := tls.Listen("tcp", *listenAddr, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *listenAddr, err)
	}
	defer listener.Close()
	log.Printf("VPN Server listening on %s", *listenAddr)

	// Handle Graceful Shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down...")
		if server.ForwardAddr == "" {
			cleanupNAT("10.8.0.0/24")
		}
		os.Exit(0)
	}()

	// 4. Accept Loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go server.handleClient(conn)
	}
}

func (s *ServerConfig) routeTunToClients() {
	buf := make([]byte, 2000)
	for {
		n, err := s.TunDevice.Read(buf)
		if err != nil {
			log.Printf("Error reading from TUN: %v", err)
			return
		}

		// Parse Destination IP (IPv4)
		// Byte 16-19 are Dest IP
		if n < 20 {
			continue
		}
		destIP := net.IP(buf[16:20])
		destIPStr := destIP.String()

		s.ClientsLock.RLock()
		conn, ok := s.Clients[destIPStr]
		s.ClientsLock.RUnlock()

		if ok {
			// Encapsulate and send
			packetData, err := protocol.Encapsulate(protocol.MsgTypeData, buf[:n])
			if err != nil {
				log.Printf("Error encapsulating packet: %v", err)
				continue
			}
			_, err = conn.Write(packetData)
			if err != nil {
				log.Printf("Error writing to client %s: %v", destIPStr, err)
			}
		} else {
			// Packet for unknown client or broadcast (ignore for now)
		}
	}
}

func (s *ServerConfig) handleClient(conn net.Conn) {
	defer conn.Close()
	log.Printf("New client connected: %s", conn.RemoteAddr())

	// RELAY MODE
	if s.ForwardAddr != "" {
		handleRelay(conn, s.ForwardAddr)
		return
	}

	// EXIT NODE MODE
	// Allocate IP
	s.ClientsLock.Lock()
	clientIP := s.NextIP
	s.NextIP = ipIncrement(s.NextIP)
	s.Clients[clientIP.String()] = conn
	s.ClientsLock.Unlock()

	defer func() {
		s.ClientsLock.Lock()
		delete(s.Clients, clientIP.String())
		s.ClientsLock.Unlock()
		log.Printf("Client %s disconnected", clientIP.String())
	}()

	log.Printf("Assigned IP: %s", clientIP.String())

	// Send Handshake (Assigned IP)
	ipConfig := fmt.Sprintf("%s/24", clientIP.String())
	handshakePacket, _ := protocol.Encapsulate(protocol.MsgTypeHandshake, []byte(ipConfig))
	conn.Write(handshakePacket)

	// Read Loop (Client -> TUN)
	for {
		packet, err := protocol.ReadPacket(conn)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from client %s: %v", clientIP.String(), err)
			}
			return
		}

		if packet.Header.Type == protocol.MsgTypeData {
			// Write to TUN
			_, err := s.TunDevice.Write(packet.Payload)
			if err != nil {
				log.Printf("Error writing to TUN: %v", err)
				return
			}
		}
	}
}

// handleRelay pipes traffic between source and destination (Next Hop)
// It performs a new TLS handshake with the next hop.
func handleRelay(srcConn net.Conn, forwardAddr string) {
	// Connect to the next hop
	tlsConfig := &tls.Config{InsecureSkipVerify: true} // TODO: Use CA
	dstConn, err := tls.Dial("tcp", forwardAddr, tlsConfig)
	if err != nil {
		log.Printf("Failed to dial next hop %s: %v", forwardAddr, err)
		return
	}
	defer dstConn.Close()

	log.Printf("Relaying connection: %s -> %s", srcConn.RemoteAddr(), forwardAddr)

	// Bidirectional Copy
	// Since we are relaying the VPN Protocol stream (Packets), we don't need to parse them.
	// We just copy raw bytes. The TLS layer handles the encryption for this hop.
	// srcConn is already decrypted by our listener, so we are reading plaintext VPN Packets.
	// dstConn will re-encrypt them for the next hop.
	
	errChan := make(chan error, 2)
	go func() {
		_, err := io.Copy(dstConn, srcConn)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(srcConn, dstConn)
		errChan <- err
	}()

	<-errChan
	log.Printf("Relay session finished for %s", srcConn.RemoteAddr())
}

func setupNAT(cidr string) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}
	return ipt.AppendUnique("nat", "POSTROUTING", "-s", cidr, "-j", "MASQUERADE")
}

func cleanupNAT(cidr string) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}
	return ipt.Delete("nat", "POSTROUTING", "-s", cidr, "-j", "MASQUERADE")
}

func ipIncrement(ip net.IP) net.IP {
	ip = ip.To4()
	v := uint(ip[0])<<24 + uint(ip[1])<<16 + uint(ip[2])<<8 + uint(ip[3])
	v++
	v3 := byte(v & 0xFF)
	v2 := byte((v >> 8) & 0xFF)
	v1 := byte((v >> 16) & 0xFF)
	v0 := byte((v >> 24) & 0xFF)
	return net.IPv4(v0, v1, v2, v3)
}

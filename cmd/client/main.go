package main

import (
	"crypto/tls"
	"d58-vpn/pkg/tunnel"
	"flag"
	"io"
	"log"
)

func main() {
	// 1. Parse Arguments
	serverAddr := flag.String("server", "localhost:443", "VPN Server address (host:port)")
	// localIP := flag.String("ip", "10.0.0.2/24", "Virtual IP for this client") // Not used in skeleton yet
	flag.Parse()

	// 2. Configure TLS
	// In a real scenario, you'd load a CA cert to verify the server, 
	// or use InsecureSkipVerify: true for testing with self-signed certs.
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // TODO: Remove this in production! Use proper CA.
	}

	// 3. Connect to Server
	log.Printf("Connecting to server at %s...", *serverAddr)
	conn, err := tls.Dial("tcp", *serverAddr, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	log.Println("Connected to VPN server")

	// 4. Setup TUN Interface
	// TODO: Create/Open local TUN interface (e.g. "tun0")
	// TODO: Configure IP address on the interface
	
	// Mock TUN for skeleton
	var tunDevice io.ReadWriteCloser 
	if tunDevice == nil {
		log.Println("TUN device not implemented in skeleton. Client will idle.")
		select {}
	}

	// 5. Start Tunnel
	t := tunnel.NewTunnel(conn, tunDevice)
	t.Start()
	
	// Keep the main goroutine running
	select {}
}

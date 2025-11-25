package main

import (
	"crypto/tls"
	"d58-vpn/pkg/tunnel"
	"flag"
	"io"
	"log"
	"net"
)

func main() {
	listenAddr := flag.String("listen", ":443", "Address to listen on (e.g. :443)")
	forwardAddr := flag.String("forward", "", "Next hop address (e.g. 10.0.0.2:443). If empty, acts as exit node.")
	certFile := flag.String("cert", "server.crt", "Path to TLS certificate")
	keyFile := flag.String("key", "server.key", "Path to TLS private key")
	flag.Parse()

	// 2. Setup TLS
	cer, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		log.Fatalf("Failed to load key pair: %v", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cer}}

	// 3. Setup TUN Interface (Only needed if we are the Exit Node)
	var tunDevice io.ReadWriteCloser
	if *forwardAddr == "" {
		// We are the exit node. We need a TUN interface.
		log.Println("Operating in EXIT NODE mode.")
		// TODO: Initialize real TUN device here (e.g. songgao/water)
		// tunDevice = openTunDevice()
	} else {
		log.Printf("Operating in RELAY mode. Forwarding to %s", *forwardAddr)
	}

	// 4. Listen for connections
	listener, err := tls.Listen("tcp", *listenAddr, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *listenAddr, err)
	}
	defer listener.Close()
	log.Printf("VPN Server listening on %s", *listenAddr)

	// 5. Connection Loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleClient(conn, *forwardAddr, tunDevice)
	}
}

func handleClient(conn net.Conn, forwardAddr string, tunDevice io.ReadWriteCloser) {
	defer conn.Close()
	log.Printf("New client connected: %s", conn.RemoteAddr())

	if forwardAddr != "" {
		// RELAY MODE: Forward traffic to the next hop
		handleRelay(conn, forwardAddr)
	} else {
		// EXIT MODE: Decapsulate and write to TUN
		// Note: tunDevice is nil in this skeleton, so this would panic if run.
		// In a real impl, tunDevice must be valid.
		if tunDevice == nil {
			log.Println("Error: TUN device not initialized (skeleton mode)")
			return
		}
		t := tunnel.NewTunnel(conn, tunDevice)
		t.Start()
		// Wait for tunnel to finish (optional, depending on t.Start impl)
		// t.Start() spawns goroutines, so we might return here. 
		// If we return, defer conn.Close() fires. 
		// tunnel.go manages closing, so we should be careful not to double close 
		// or close prematurely. 
		// For this skeleton, we'll let the tunnel manage the connection.
		select {} // Block to keep connection open if t.Start() is async
	}
}

func handleRelay(srcConn net.Conn, forwardAddr string) {
	// Connect to the next hop
	// TODO: Load CA cert for verification, or use InsecureSkipVerify for testing
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	
	dstConn, err := tls.Dial("tcp", forwardAddr, tlsConfig)
	if err != nil {
		log.Printf("Failed to dial next hop %s: %v", forwardAddr, err)
		return
	}
	defer dstConn.Close()

	log.Printf("Relaying connection: %s <-> %s", srcConn.RemoteAddr(), forwardAddr)

	// Bidirectional Copy
	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(dstConn, srcConn)
		errChan <- err
	}()

	go func() {
		_, err := io.Copy(srcConn, dstConn)
		errChan <- err
	}()

	// Wait for first error or disconnect
	<-errChan
	log.Printf("Relay session finished for %s", srcConn.RemoteAddr())
}

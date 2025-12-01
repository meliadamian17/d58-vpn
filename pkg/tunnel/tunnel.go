package tunnel

import (
	"d58-vpn/pkg/protocol"
	"io"
	"log"
	"net"
)

// Tunnel represents an active VPN session.
type Tunnel struct {
	ClientAddr string
	Conn       net.Conn           // The TCP/TLS connection
	Tun        io.ReadWriteCloser // The TUN/TAP interface
	Done       chan struct{}      // Closed when tunnel ends
}

// NewTunnel creates a new Tunnel instance.
func NewTunnel(conn net.Conn, tun io.ReadWriteCloser) *Tunnel {
	return &Tunnel{
		ClientAddr: conn.RemoteAddr().String(),
		Conn:       conn,
		Tun:        tun,
		Done:       make(chan struct{}),
	}
}

// Start begins the traffic relay between the network connection and the TUN interface.
func (t *Tunnel) Start() {
	log.Printf("Starting tunnel for %s", t.ClientAddr)

	// Network -> TUN
	go t.netToTun()

	// TUN -> Network
	go t.tunToNet()
}

// netToTun reads encrypted/encapsulated packets from the network, decapsulates them, and writes to TUN.
func (t *Tunnel) netToTun() {
	log.Println("netToTun routine started")
	defer func() {
		log.Println("netToTun routine exiting")
		// Close the connection to stop the other goroutine
		t.Conn.Close()
		// Signal completion
		select {
		case <-t.Done:
		default:
			close(t.Done)
		}
	}()

	for {
		// 1. Read Packet from Network
		packet, err := protocol.ReadPacket(t.Conn)
		if err != nil {
			// Normal disconnect or error
			log.Printf("Network read error: %v", err)
			return
		}

		if packet.Header.Type == protocol.MsgTypeData {
			// 2. Write to TUN
			_, err = t.Tun.Write(packet.Payload)
			if err != nil {
				log.Printf("Error writing to TUN: %v", err)
				return
			}
		}
		// Ignore KeepAlive or Handshake in data stream
	}
}

// tunToNet reads raw IP packets from TUN, encapsulates them, and writes to the network.
func (t *Tunnel) tunToNet() {
	log.Println("tunToNet routine started")
	defer func() {
		log.Println("tunToNet routine exiting")
		t.Conn.Close()
		select {
		case <-t.Done:
		default:
			close(t.Done)
		}
	}()

	buf := make([]byte, 2000) 
	for {
		n, err := t.Tun.Read(buf)
		if err != nil {
			log.Printf("Error reading from TUN: %v", err)
			return
		}

		packetData, err := protocol.Encapsulate(protocol.MsgTypeData, buf[:n])
		if err != nil {
			log.Printf("Error encapsulating packet: %v", err)
			continue
		}

		_, err = t.Conn.Write(packetData)
		if err != nil {
			log.Printf("Error writing to network: %v", err)
			return
		}
	}
}

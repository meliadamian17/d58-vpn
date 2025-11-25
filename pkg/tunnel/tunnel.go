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
	Conn       net.Conn          // The TCP/TLS connection
	Tun        io.ReadWriteCloser // The TUN/TAP interface
	// TODO: maybe add fields for encryption fields if using crypto 
}

// NewTunnel creates a new Tunnel instance.
func NewTunnel(conn net.Conn, tun io.ReadWriteCloser) *Tunnel {
	return &Tunnel{
		ClientAddr: conn.RemoteAddr().String(),
		Conn:       conn,
		Tun:        tun,
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
	defer t.Conn.Close()
	// defer t.Tun.Close() // Typically we don't close TUN on single client disconnect in server mode, but for 1:1 we might.

	for {
		// 1. Read Packet from Network
		packet, err := protocol.ReadPacket(t.Conn)
		if err != nil {
			log.Printf("Error reading from network: %v", err)
			return
		}

		// TODO: Decrypt payload if using custom encryption (NaCl/AES)
		// decrypted := decrypt(packet.Payload)

		// 2. Write to TUN
		// TODO: Handle IP packet verification/filtering
		_, err = t.Tun.Write(packet.Payload)
		if err != nil {
			log.Printf("Error writing to TUN: %v", err)
			return
		}
	}
}

// tunToNet reads raw IP packets from TUN, encapsulates them, and writes to the network.
func (t *Tunnel) tunToNet() {
	defer t.Conn.Close()

	buf := make([]byte, 1500) // Standard MTU
	for {
		// 1. Read from TUN
		n, err := t.Tun.Read(buf)
		if err != nil {
			log.Printf("Error reading from TUN: %v", err)
			return
		}

		// TODO: Encrypt payload if using custom encryption (NaCl/AES)
		// encrypted := encrypt(buf[:n])

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


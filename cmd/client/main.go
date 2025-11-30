package main

import (
	"crypto/tls"
	"d58-vpn/pkg/nettools"
	"d58-vpn/pkg/protocol"
	"d58-vpn/pkg/tunnel"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/vishvananda/netlink"
)

func main() {
	serverAddr := flag.String("server", "localhost:443", "VPN Server address (host:port)")
	flag.Parse()

	if os.Geteuid() != 0 {
		log.Fatal("Client must run as root to manage TUN/Routing.")
	}

	// Resolve Server IP for routing rules later
	tcpAddr, err := net.ResolveTCPAddr("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("Failed to resolve server address: %v", err)
	}
	serverIP := tcpAddr.IP

	// 2. Configure TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // since we're using self-signed certs, want to ignore these things 
	}

	// 3. Connect to Server
	log.Printf("Connecting to server at %s...", *serverAddr)
	conn, err := tls.Dial("tcp", *serverAddr, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()
	log.Println("Connected to VPN server. Waiting for handshake...")

	// 4. Handshake: Receive Assigned IP
	packet, err := protocol.ReadPacket(conn)
	if err != nil {
		log.Fatalf("Failed to receive handshake: %v", err)
	}
	if packet.Header.Type != protocol.MsgTypeHandshake {
		log.Fatalf("Expected handshake packet, got type %d", packet.Header.Type)
	}
	assignedIPCIDR := string(packet.Payload)
	log.Printf("Assigned IP: %s", assignedIPCIDR)

	// 5. Setup TUN Interface
	tunName := "tun0"
	tunDevice, _, err := nettools.CreateTUN(tunName, assignedIPCIDR)
	if err != nil {
		log.Fatalf("Failed to create TUN: %v", err)
	}
	defer tunDevice.Close()

	// 6. Setup Routing 
	gwIP, gwIfName, err := nettools.GetDefaultGateway()
	if err != nil {
		log.Printf("Warning: Could not detect default gateway: %v. Routing might fail.", err)
	}

	// Prepare Cleanup Function
	cleanup := func() {
		log.Println("Cleaning up routes...")
		if gwIfName != "" {
			// Remove the specific server route
			oldLink, _ := netlink.LinkByName(gwIfName)
			if oldLink != nil {
				serverRoute := &netlink.Route{
					Dst:       &net.IPNet{IP: serverIP, Mask: net.CIDRMask(32, 32)},
					Gw:        gwIP,
					LinkIndex: oldLink.Attrs().Index,
				}
				netlink.RouteDel(serverRoute)
			}
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Received signal. Shutting down...")
		cleanup()
		os.Exit(0)
	}()

	if gwIfName != "" {
		log.Printf("Current Gateway: %s on %s", gwIP, gwIfName)
	}

	// Apply VPN Routes
	if err := applyVPNRoutes(gwIP, gwIfName, serverIP, tunName); err != nil {
		log.Fatalf("Failed to apply VPN routes: %v", err)
	}

	// 7. Start Tunnel
	t := tunnel.NewTunnel(conn, tunDevice)
	t.Start()

	// Block until tunnel error/disconnect
	<-t.Done
	log.Println("Tunnel connection lost.")
	cleanup()
}

func applyVPNRoutes(oldGw net.IP, oldGwIf string, serverIP net.IP, tunName string) error {
	// 1. Add route to VPN Server via old Gateway (ONLY if we have a gateway)
	if oldGwIf != "" {
		oldLink, err := netlink.LinkByName(oldGwIf)
		if err != nil {
			return err
		}

		serverRoute := &netlink.Route{
			Dst:       &net.IPNet{IP: serverIP, Mask: net.CIDRMask(32, 32)},
			Gw:        oldGw,
			LinkIndex: oldLink.Attrs().Index,
		}
		if err := netlink.RouteAdd(serverRoute); err != nil {
			log.Printf("Note: Failed to add server route (might exist): %v", err)
		}
	}

	// 2. Add default route override (Def1)
	tunLink, err := netlink.LinkByName(tunName)
	if err != nil {
		return err
	}

	// 0.0.0.0/1
	_, cidr1, _ := net.ParseCIDR("0.0.0.0/1")
	r1 := &netlink.Route{
		Dst:       cidr1,
		LinkIndex: tunLink.Attrs().Index,
	}
	if err := netlink.RouteAdd(r1); err != nil {
		return err
	}

	// 128.0.0.0/1
	_, cidr2, _ := net.ParseCIDR("128.0.0.0/1")
	r2 := &netlink.Route{
		Dst:       cidr2,
		LinkIndex: tunLink.Attrs().Index,
	}
	if err := netlink.RouteAdd(r2); err != nil {
		return err
	}

	// 3. Add Specific Route for the VPN Internal Subnet (10.8.0.0/24)
	// This ensures traffic destined for the VPN peers themselves goes through the tunnel
	_, vpnCidr, _ := net.ParseCIDR("10.8.0.0/24")
	r3 := &netlink.Route{
		Dst:       vpnCidr,
		LinkIndex: tunLink.Attrs().Index,
	}
	// This might overlap with the 0/1 and 128/1 routes, but explicit is good.
	if err := netlink.RouteAdd(r3); err != nil {
		// Ignore if exists
	}

	log.Println("Routes applied. Traffic is now tunneling.")
	return nil
}

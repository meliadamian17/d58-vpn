package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	fmt.Println("VPN Server starting...")

	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	fmt.Println("VPN Server listening on :8080")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		fmt.Printf("Accepted connection from %s\n", conn.RemoteAddr())
		conn.Close()
	}
}
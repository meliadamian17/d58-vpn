package main

import (
	"fmt"
	"log"
	"net"
	"time"
)

func main() {
	fmt.Println("VPN Client starting...")

	serverAddr := "localhost:8080"

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server %s: %v", serverAddr, err)
	}
	defer conn.Close()

	fmt.Printf("Connected to VPN server at %s\n", serverAddr)
	time.Sleep(1 * time.Second)
	fmt.Println("Client closing connection.")
}
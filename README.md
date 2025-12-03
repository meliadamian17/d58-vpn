# D58 VPN Project 
## 📘 Project Description

A **Virtual Private Network (VPN)** creates a secure, encrypted tunnel between clients and a server, enabling private communication over untrusted networks.  
This project implements a simplified VPN system that demonstrates core networking and security concepts from **CSCD58**.

The implementation focuses on:

- **Point-to-point encrypted communication**  
- **IP packet encapsulation**  
- **Tunnel creation and management**

By bridging theoretical concepts with hands-on development, this project showcases core principles of **protocol design**, **encryption**, **authentication**, and **packet processing** within a secure networked environment.

## 🎯 Specific Goals and Targets

- Implement a **TLS/SSL-based encrypted tunnel** between the client and server  
- Design and implement a **custom tunneling protocol** for encapsulating IP packets  
- Develop **authentication mechanisms** (certificate-based and credential-based)  
- Create **packet routing and forwarding logic** within the encrypted tunnel  
- Implement **connection management** and **session lifecycle handling**  
- Build a **configuration system** for VPN clients and servers  
- Develop **monitoring and diagnostic tools** for tunnel status and traffic analysis  
- Demonstrate the system with **multiple clients** connecting through a **single VPN server**

---
# Setup & Usage

This guide provides step-by-step instructions to set up, run, and verify the D58 VPN project using Docker and Mininet.

## Prerequisites

- **Docker Desktop** (running and accessible)
- **Windows (WSL2)** or **Linux** environment

## 1. Quick Start (Automated Setup)

We have provided a portable script that handles the building of the Docker image and starting the container with the necessary privileges.

1. Open your terminal
2. Clone the repository
    ```bash
    git clone https://github.com/meliadamian17/d58-vpn.git
    ```
3. Run the startup script:

   **Bash / WSL / Linux:**
   ```bash
   cd d58-vpn
   chmod +x ./run.sh
   sudo ./run.sh
   ```
   *(Note: This builds the `vpn-mininet` Docker image if it doesn't exist and drops you into a shell inside the container).*

---

## 2. Running the Network Topology

Once you are inside the Docker container shell (`root@<id>:/app#`), follow these steps to spin up the network topology and test the VPN.

1. **Start the Mininet Topology:**
   Run the Python script included in the image. This sets up the network (Server `h1` <-> Switch `s1` <-> Client `h2`), enables NAT for internet access, and starts the VPN Server on `h1`.

   ```bash
   python topology.py
   ```

2. **Wait for Initialization:**
   - The script will initialize Open vSwitch.
   - It will perform a `pingall` to verify physical network connectivity.
   - It will start the VPN Server on `h1` and print the log path (`/tmp/vpn-server.log`).
   - You will see the Mininet CLI prompt: `mininet>`.

---

## 3. Verifying the VPN

The topology script automatically starts the VPN client and establishes the tunnel. Once you see the Mininet CLI prompt (`mininet>`), the VPN should already be active. Follow these steps to verify everything is working correctly.

### Step A: Verify Client Connection
Check that the VPN client successfully connected and received a tunnel IP.
```bash
mininet> h2 ip addr show tun0
```

*Expected Output:*
You should see the `tun0` interface in the UP state with an assigned IP address like `10.8.0.2/24`.

**Optional:** View the detailed client logs:
```bash
mininet> h2 cat /tmp/vpn-client.log
```

*You should see:*
- "Connected to VPN server. Waiting for handshake..."
- "Assigned IP: 10.8.0.x/24"
- "Routes applied. Traffic is now tunneling."

### Step B: Verify Routing (Kill Switch)
Check the routing table on `h2` to ensure the default route has been overridden to use the tunnel (`tun0`).

```bash
mininet> h2 route -n
```

*Expected Output:*
You should see routes for `0.0.0.0/1` and `128.0.0.0/1` pointing to interface **`tun0`**. This ensures all traffic goes through the VPN.

### Step C: Verify Traffic Flow (Traceroute)
Trace the path to an internet address (e.g., Google DNS `8.8.8.8`).

```bash
mininet> h2 traceroute -I 8.8.8.8
```

*Expected Output:*
- **Hop 1:** `10.8.0.1` (This is the VPN Server's virtual IP). **This proves traffic is inside the tunnel.**
- **Hop 2:** `10.0.0.3` (The NAT gateway for the simulation).
- **Hop 3+:** `172.x.x.x` (Docker gateway and out to the real internet).

### Step D: Verify Internet Connectivity
Check if `h2` can actually reach the internet through the VPN.

```bash
mininet> h2 curl -I https://www.google.com
```

*Expected Result:* `HTTP/2 200` (or similar success status).

---

## 4. Cleaning Up

To exit the simulation:
1. Type `exit` in the Mininet CLI.
2. The container stops automatically (if run via `run.sh` with `--rm`).

## 5. Advanced Topologies & Automated Testing

The `topology.py` script supports multiple topologies and automated verification modes.

### Run Automated Tests (Simple Topology)
Verifies client connectivity, routing, and **encryption** (using tcpdump to ensure no plaintext leaks).

```bash
python topology.py --test
```

### Run Relay Topology
Sets up a 3-hop chain: `Client` -> `Relay Node` -> `Exit Node`.
This verifies that your VPN server can act as a relay, masking the client's IP from the Exit Node.

```bash
python topology.py --topo relay --test
```

*Verification Check:* The test will check the Exit Node's logs to ensure the incoming connection originates from the **Relay's IP**, not the Client's IP.

---

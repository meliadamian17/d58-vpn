# D58 VPN Project 

This guide provides step-by-step instructions to set up, run, and verify the D58 VPN project using Docker and Mininet.

## Prerequisites

- **Docker Desktop** (running and accessible)
- **Windows (WSL2)** or **Linux** environment

## 1. Quick Start (Automated Setup)

We have provided a portable script that handles the building of the Docker image and starting the container with the necessary privileges.

1. Open your terminal 
2. Run the startup script:

   **Bash / WSL / Linux:**
   ```bash
   ./run.sh
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

Perform the following steps inside the `mininet>` CLI to confirm the VPN works and traffic is routed securely.

### Step A: Start the VPN Client
Start the client on host `h2` connecting to the server on `h1` (`10.0.0.1`). We run it in the background (`&`) so we can keep using the CLI.

```bash
mininet> h2 /usr/local/bin/vpn-client -server 10.0.0.1:443 &
```

*Expected Output:* You should see logs indicating:
- "Connected to VPN server"
- "Assigned IP: 10.8.0.x"
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
mininet> h2 traceroute -n 8.8.8.8
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
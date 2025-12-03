# D58 VPN Project Overview

## 1\. Project Overview

This project implements a TLS-based VPN with the following components:

### VPN Client

* Creates a TUN interface
* Establishes a TLS connection to a VPN server
* Overrides routing so that all traffic (`0.0.0.0/1` + `128.0.0.0/1`) goes through the tunnel

### VPN Server

The server can operate in two modes:

* **Exit node mode**: Terminates the tunnel and forwards decrypted packets to the real internet using NAT
* **Relay node mode**: Forwards the encrypted VPN stream to another VPN node (multi-hop chain)

### Custom VPN Protocol

Built on top of TLS with:

- Lightweight framing for Handshake, Data, and KeepAlive messages

### Mininet Test Harness

Includes topology and automated tests:

* **Simple topology**: `Client ↔ Exit`
* **Relay topology**: `Client ↔ Relay ↔ Exit`

Automated tests verify:

* Routing through the tunnel
* Encryption (no plaintext ICMP leaks)
* Relay anonymity (exit only sees relay's IP)

---

## 2\. Component Documentation

### 2.1 VPN Client (`client/main.go`)

#### `main()`

**Purpose**: Entry point for the VPN client. Establishes a secure connection to the server, configures the TUN interface, installs VPN routes, and starts tunneling packets.

**Implementation Details**:

1. **Argument parsing \& privilege check**

   * Uses `flag.String("server", "localhost:443", ...)` to get `-server host:port`
   * Exits if `os.Geteuid() != 0` (must be root to manage TUN and routes)

2. **Resolve server address**

   * `net.ResolveTCPAddr("tcp", \*serverAddr)` → obtain `serverIP` for later routing (`applyVPNRoutes`)

3. **TLS configuration \& connection**

   * Builds `tls.Config{ InsecureSkipVerify: true }` (self-signed certs)
   * `tls.Dial("tcp", \*serverAddr, tlsConfig)` to connect to server

4. **Handshake: receive assigned IP**

   * Calls `protocol.ReadPacket(conn)` and expects `Header.Type == MsgTypeHandshake`
   * `assignedIPCIDR := string(packet.Payload)` (e.g., `"10.8.0.3/24"`)

5. **TUN setup**

   * Uses `nettools.CreateTUN("tun0", assignedIPCIDR)` to:

     * Create a TUN device
     * Assign `assignedIPCIDR` to it
     * Set MTU to 1300 and bring interface up

6. **Default gateway discovery**

   * Calls `nettools.GetDefaultGateway()` to discover current default gateway IP and interface name
   * Used to:

     * Keep the path to the VPN server reachable outside the tunnel
     * Override default routes correctly

7. **Cleanup handler**

   * Defines `cleanup()` which removes the specific host route that was added for the VPN server (`serverIP/32` via old gateway)
   * Registers a signal handler (`os.Signal`, `os.Interrupt`, `syscall.SIGTERM`) to call `cleanup()` and exit gracefully

8. **Apply VPN routes**

   * Invokes `applyVPNRoutes(gwIP, gwIfName, serverIP, tunName)`, which:

     * Adds a host route for the VPN server via the original gateway
     * Adds `0.0.0.0/1` and `128.0.0.0/1` via the TUN interface
     * Adds `10.8.0.0/24` via TUN as an explicit internal VPN subnet route

9. **Start the tunnel**

   * Constructs `tunnel.NewTunnel(conn, tunDevice)` and calls `t.Start()`
   * Blocks on `<-t.Done` until the tunnel ends, then performs cleanup

**Required Functionality**:

* Establishes a secure TLS control + data channel to the server
* Installs a full-tunnel VPN (all default traffic goes through the TUN interface)
* Maintains reachability to the VPN server itself via the old gateway
* Handles graceful teardown and route cleanup on disconnect or signal

---

#### `applyVPNRoutes(oldGw net.IP, oldGwIf string, serverIP net.IP, tunName string)`

**Purpose**: Installs the routing entries needed for a full-tunnel VPN while ensuring the VPN server remains reachable via the original gateway.

**Implementation Details**:

1. **Ensure server remains reachable**

   * If `oldGwIf` is not empty:

     * Looks up link using `netlink.LinkByName(oldGwIf)`
     * Creates `netlink.Route` with:

       * `Dst = serverIP/32`
       * `Gw = oldGw`
       * `LinkIndex = oldLink.Attrs().Index`

     * Calls `netlink.RouteAdd(serverRoute)` (errors are logged but tolerated)

2. **Install default-splitting routes via TUN (def1 style)**

   * Looks up TUN link with `netlink.LinkByName(tunName)`
   * Adds `0.0.0.0/1` route → `r1` (via TUN)
   * Adds `128.0.0.0/1` route → `r2` (via TUN)
   * These two `/1` routes together cover the whole IPv4 space, effectively overriding the default route to use the tunnel

3. **Route for internal VPN subnet**

   * Adds `10.8.0.0/24` route via TUN
   * Even though this is already covered by the `/1` routes, it explicitly declares the VPN internal network
   * Logs: `"Routes applied. Traffic is now tunneling."`

**Required Functionality**:

* Provides the kill-switch + full-tunnel semantics:

  * All general internet traffic is forced into the tunnel
  * Only the VPN server's IP is still reachable outside the tunnel via the original gateway

* Ensures internal VPN addresses are routed through the tunnel

---

### 2.2 VPN Server (`server/main.go`)

#### `type ServerConfig`

**Purpose**: Holds server state required to manage multiple clients and keep track of tunnel endpoints.

**Fields**:

* `TunDevice \*water.Interface` — TUN device attached on the exit node
* `Clients map\[string]net.Conn` — mapping from client VPN IP string → client connection
* `ClientsLock sync.RWMutex` — protects the Clients map
* `NextIP net.IP` — next IP to assign to a connecting client (starts at `10.8.0.2`)
* `ForwardAddr string` — if non-empty, server acts as a relay and forwards decrypted VPN stream to the next hop

---

#### `main()` (server)

**Purpose**: Entry point for VPN server. Configures exit/relay mode, sets up TUN \& NAT, starts TLS listener, and accepts incoming clients.

**Implementation Details**:

1. **Flags / settings**

   * `-listen` (e.g., `:443`)
   * `-forward` (e.g., `1.2.3.4:443`) — if set → relay mode
   * `-cert`, `-key` — TLS cert + key paths

2. **Root check**

   * Exits if not root (`os.Geteuid() != 0`)

3. **Initialize server state**

   * `NextIP = 10.8.0.2`
   * `Clients` map for exit node mode
   * `ForwardAddr` from CLI

4. **Exit node mode** (`ForwardAddr == ""`)

   * Logs `"Mode: EXIT NODE (NAT Enabled)"`
   * Enables IP forwarding: `nettools.EnableIPForwarding()`
   * Creates TUN:

     * `nettools.CreateTUN("tun0", "10.8.0.1/24")`
     * Stores in `server.TunDevice`

   * Sets up NAT:

     * `setupNAT("10.8.0.0/24")` → add iptables POSTROUTING MASQUERADE rule
     * Defers `cleanupNAT` at shutdown

   * Starts `go server.routeTunToClients()` to forward TUN traffic to clients

5. **Relay mode** (`ForwardAddr != ""`)

   * Logs `"Mode: RELAY NODE (Forwarding to X)"`
   * No TUN or NAT; acts as a hop in a chain

6. **TLS configuration**

   * Loads certificate + key via `tls.LoadX509KeyPair`
   * Creates `tls.Config{Certificates: \[]tls.Certificate{cer}}`

7. **TLS listener**

   * `tls.Listen("tcp", \*listenAddr, tlsConfig)`
   * Registers a signal handler to cleanup NAT on exit
   * Accept loop: For each incoming connection, starts `go server.handleClient(conn)`

**Required Functionality**:

* Implements both:

  * **VPN exit server** that:

    * Allocates client IPs in `10.8.0.0/24`
    * Terminates tunnels on a TUN device
    * Uses NAT to forward decrypted traffic to the real internet

  * **Relay server** that:

    * Does not terminate TUN/TAP
    * Simply relays encrypted VPN protocol stream to the next node

---

#### `func (s \*ServerConfig) routeTunToClients()`

**Purpose**: Reads decrypted IP packets from the TUN interface and sends them to the appropriate client connection based on the destination IP.

**Implementation Details**:

* Allocates a buffer `buf := make(\[]byte, 2000)`
* Loop:

  * Reads from `s.TunDevice.Read(buf)`
  * Skips packets with `n < 20` (needs minimal IPv4 header)
  * Extracts destination IP from bytes 16–19 in the IP header
  * Converts to string `destIPStr`
  * Client lookup:

    * Acquires `ClientsLock.RLock()`
    * Looks up `conn := s.Clients\[destIPStr]`
    * Releases lock

  * If client found:

    * Uses `protocol.Encapsulate(protocol.MsgTypeData, buf\[:n])`
    * Writes encapsulated packet to the client's `conn`

  * If client not found:

    * Ignores packet (broadcast / unknown client)

**Required Functionality**:

* Implements the exit-node half of the VPN:

  * TUN → client mapping uses logical VPN IPs
  * Only packets destined for known VPN clients are forwarded

---

#### `func (s \*ServerConfig) handleClient(conn net.Conn)`

**Purpose**: Handles a single client connection for both relay mode and exit mode.

**Implementation Details**:

1. Logs `"New client connected: <remote-addr>"`
2. **Relay mode** (`s.ForwardAddr != ""`)

   * Immediately calls `handleRelay(conn, s.ForwardAddr)` and returns
   * No TUN, no IP assignment

3. **Exit node mode**:

   * **Allocates a VPN IP**:

     * Lock `ClientsLock.Lock()`
     * `clientIP := s.NextIP`
     * `s.NextIP = ipIncrement(s.NextIP)`
     * `s.Clients\[clientIP.String()] = conn`
     * Unlock

   * **Defer cleanup**: Remove client from map on disconnect
   * Logs `Assigned IP: <clientIP>`
   * **Handshake**:

     * Prepare `ipConfig := "<ip>/24"`
     * Encapsulate as `MsgTypeHandshake` using `protocol.Encapsulate`
     * Write handshake packet to client

   * **Data loop**:

     * Repeatedly calls `protocol.ReadPacket(conn)`
     * For `MsgTypeData`, writes payload to `s.TunDevice.Write(packet.Payload)`
     * On error/EOF, exits and triggers cleanup

**Required Functionality**:

* Per-client IP allocation in the `10.8.0.0/24` VPN subnet
* Sending initial handshake to client with their assigned CIDR
* Forwarding decrypted IP traffic from client → TUN (and then TUN → clients via `routeTunToClients`)

---

#### `func handleRelay(srcConn net.Conn, forwardAddr string)`

**Purpose**: Implements a VPN relay node that forwards the VPN protocol stream to another VPN node (next hop), creating a multi-hop chain.

**Implementation Details**:

* Builds `tls.Config{InsecureSkipVerify: true}`
* `tls.Dial("tcp", forwardAddr, tlsConfig)` to connect to the next hop (relay or exit)
* Logs `"Relaying connection: src -> forwardAddr"`
* Uses `io.Copy` in both directions:

  * Goroutine 1: `io.Copy(dstConn, srcConn)`
  * Goroutine 2: `io.Copy(srcConn, dstConn)`

* Uses `errChan` to wait for either direction to fail, then closes session

**Required Functionality**:

* Creates a pure relay hop:

  * Outer TLS connection from client → relay
  * New TLS connection from relay → next hop
  * Relay blindly pipes VPN packets between them
  * The exit node only sees the IP of the relay as its client, not the original client's IP

---

#### `setupNAT(cidr string)` / `cleanupNAT(cidr string)`

**Purpose**: Configures and removes NAT so that traffic from the VPN subnet can reach the internet using IP masquerading.

**Implementation Details**:

* Uses `github.com/coreos/go-iptables/iptables`
* **setupNAT**:

  * Calls `iptables.New()`
  * Appends a rule to table `nat`, chain `POSTROUTING`:

    * `-s <cidr> -j MASQUERADE`

* **cleanupNAT**:

  * Deletes the same rule from `POSTROUTING`

**Required Functionality**:

* Enables:

  * Exit node to source-NAT all traffic from `10.8.0.0/24` to its own public interface
  * Ensures return traffic from the internet comes back through the VPN server

---

### 2.3 Networking Tools (`pkg/nettools/nettools.go`)

#### `CreateTUN(name string, ipCIDR string)`

**Purpose**: Creates and configures a TUN device with a specific name and IP/CIDR.

**Implementation Details**:

* Uses `github.com/songgao/water`:

  * `water.Config{DeviceType: water.TUN}` and `PlatformSpecificParams{Name: name}`
  * `water.New(config)` to create TUN interface

* Obtains a `netlink.Link` for the interface name
* Parses `ipCIDR` into `ip` and `ipNet`
* Adds IP address using `netlink.AddrAdd`
* Sets MTU to 1300 via `netlink.LinkSetMTU`
* Brings link up via `netlink.LinkSetUp`
* Returns `(ifce, link, nil)`

**Required Functionality**:

* Provides a reusable helper to:

  * Create a TUN interface
  * Assign IP/Netmask
  * Prepare it for use by the VPN client/server

---

#### `EnableIPForwarding()`

**Purpose**: Enables IPv4 forwarding on the host so the exit node can forward packets between TUN and physical interfaces.

**Implementation Details**:

* Runs `sysctl -w net.ipv4.ip\_forward=1` via `exec.Command`
* Returns an error if the sysctl fails

**Required Functionality**:

* Required for exit node to behave as a router/NAT

---

#### `GetDefaultGateway()`

**Purpose**: Detects the default IPv4 gateway and its interface.

**Implementation Details**:

* Uses `netlink.RouteList(nil, 4)` to list IPv4 routes
* Looks for a route with `route.Dst == nil` → default route
* Uses `netlink.LinkByIndex(route.LinkIndex)` to get interface name
* Returns `(route.Gw, ifaceName, nil)`

**Required Functionality**:

* Crucial for the client to:

  * Preserve reachability to the VPN server via the original gateway
  * Override default routing without disconnecting itself from the server

---

### 2.4 VPN Protocol (`pkg/protocol/protocol.go`)

#### Types and Constants

**Message types**:

* `MsgTypeData = 0x01`
* `MsgTypeKeepAlive = 0x02`
* `MsgTypeHandshake = 0x03`

**Header**:

* `Type uint8`
* `Length uint16`

**Packet**:

* `Header Header`
* `Payload \[]byte`

**HeaderSize** = 3 bytes

---

#### `Encapsulate(msgType uint8, payload \[]byte)`

**Purpose**: Wraps a raw payload into a framed packet suitable for sending over a byte-stream (TLS).

**Implementation Details**:

* Rejects if `len(payload) > 65535`
* Writes:

  * `msgType` (1 byte)
  * `uint16(len(payload))` (2 bytes, big-endian)
  * `payload` bytes

* Returns combined byte slice

**Required Functionality**:

* Provides minimal framing over TLS so that:

  * The receiver can distinguish message boundaries
  * Supports multiple message types (handshake, data, keepalive)

---

#### `ReadPacket(r io.Reader)`

**Purpose**: Reads a full packet from a stream, parsing the custom VPN header and payload.

**Implementation Details**:

* Allocates `headerBuf\[3]` and reads exactly 3 bytes (`io.ReadFull`)
* Parses:

  * `Header.Type = headerBuf\[0]`
  * `Header.Length = binary.BigEndian.Uint16(headerBuf\[1:3])`

* Allocates payload of length `Header.Length` and reads it fully
* Returns `Packet{Header, Payload}`

**Required Functionality**:

* Complements `Encapsulate` to:

  * Reassemble packet boundaries from a TLS stream
  * Allow Tunnel and server/client code to handle message types uniformly

---

### 2.5 Tunnel Logic (`pkg/tunnel/tunnel.go`)

#### `type Tunnel`

**Purpose**: Represents an active VPN session from the client's perspective, bridging between a TUN interface and a network connection.

**Fields**:

* `ClientAddr string` — remote address
* `Conn net.Conn` — TLS connection to server
* `Tun io.ReadWriteCloser` — TUN interface
* `Done chan struct{}` — closed when the tunnel stops

---

#### `NewTunnel(conn net.Conn, tun io.ReadWriteCloser)`

**Purpose**: Constructor for Tunnel.

**Implementation Details**:

* Initializes fields and Done channel
* Stores `conn.RemoteAddr().String()` in `ClientAddr`

---

#### `Start()`

**Purpose**: Starts the bidirectional forwarding between network and TUN.

**Implementation Details**:

* Logs `"Starting tunnel for ..."`
* Launches two goroutines:

  * `go t.netToTun()`
  * `go t.tunToNet()`

---

#### `netToTun()`

**Purpose**: Reads VPN packets from the network, decapsulates them, and writes raw IP packets to TUN.

**Implementation Details**:

* Loop:

  * `packet, err := protocol.ReadPacket(t.Conn)`
  * If error: logs and exits, closes Conn, closes Done
  * If `packet.Header.Type == MsgTypeData`:

    * Writes `packet.Payload` to `t.Tun`

**Required Functionality**:

* Implements the server → client direction of data:

  * Uses VPN framing to reconstruct IP packets and inject them into the client's kernel via TUN

---

#### `tunToNet()`

**Purpose**: Reads raw IP packets from TUN and sends them encapsulated over the network.

**Implementation Details**:

* Allocates `buf := make(\[]byte, 2000)`
* Loop:

  * Reads from `t.Tun.Read(buf)`
  * `protocol.Encapsulate(MsgTypeData, buf\[:n])`
  * Writes encapsulated packet to `t.Conn`
  * On error: logs, closes connection, closes Done

**Required Functionality**:

* Implements client → server direction of data, wrapping IP packets into VPN protocol frames over TLS

---

### 2.6 Topology \& Test Harness (`topology.py`)

#### `SimpleTopo`

**Purpose**: Defines the simple topology: one VPN server host and one client host connected via a switch.

**Layout**:

* `h1`: `10.0.0.1/24` (VPN server host)
* `h2`: `10.0.0.2/24` (VPN client host)
* `s1`: OVSBridge switch
* Links: `h1—s1`, `h2—s1`

---

#### `RelayTopo`

**Purpose**: Defines the relay topology: `client → relay → exit server`.

**Layout**:

* `h\_exit`: `10.0.0.1/24` (exit node)
* `h\_relay`: `10.0.0.2/24` (relay node)
* `h\_client`: `10.0.0.3/24` (VPN client)
* `s1`: OVSBridge
* Links: `h\_exit—s1`, `h\_relay—s1`, `h\_client—s1`

---

#### `generate\_certs(host, cn)`

**Purpose**: Generates a self-signed TLS cert and key on a given Mininet host.

**Implementation Details**:

* Uses `openssl req -new -newkey rsa:2048 -days 365 -nodes -x509 ... -keyout server.key -out server.crt -subj "/.../CN=<cn>"`

---

#### `start\_vpn\_server(host, mode='exit', forward\_to='')`

**Purpose**: Starts the VPN server binary on a Mininet host in either exit or relay mode.

**Implementation Details**:

* Base command: `/usr/local/bin/vpn-server -listen :443 -cert server.crt -key server.key`
* If `mode == 'relay'` adds `-forward <forward\_to>`
* Redirects stdout/stderr to `/tmp/vpn-<host>.log` and runs in background

---

#### `verify\_encryption(client, target\_ip)`

**Purpose**: Verifies that:

* No plaintext ICMP packets to the internet leak on the client's physical interface
* Encrypted TCP/443 traffic does appear on the physical interface

**Implementation Details**:

* On client:

  * Starts tcpdump on `<client>-eth0`:

    * One capture for `icmp and dst host target\_ip`
    * One capture for `tcp port 443 and dst host target\_ip`

  * Sends `ping -c 1 8.8.8.8` through the VPN
  * Reads logs:

    * If ICMP capture saw packets → FAIL (plaintext leaking)
    * If TCP/443 capture saw packets → PASS (encrypted traffic present)

---

#### `run(topo\_name='simple', do\_test=False)`

**Purpose**: Main Mininet harness to start topology, VPN servers, client, and optionally run automated tests.

**Implementation Details**:

1. Builds `SimpleTopo` or `RelayTopo` based on `--topo`
2. Creates `Mininet(switch=OVSBridge)`
3. Adds NAT using `net.addNAT().configDefault()` for external internet access
4. Starts the network, then:

   * **For simple**:

     * `server = h1`, `client = h2`, `server\_ip = 10.0.0.1`

   * **For relay**:

     * `server = h\_exit`, `relay = h\_relay`, `client = h\_client`
     * `server\_ip = 10.0.0.1`, `relay\_ip = 10.0.0.2`

5. **Physical connectivity test**:

   * `net.pingAll()` ensures the underlying topology is correct

6. **VPN setup**:

   * Generates certs, starts exit server on server
   * If relay topology:

     * Generates certs and starts relay server on `h\_relay` with `-forward server\_ip:443`
     * Client will connect to `relay\_ip` instead of directly to exit

7. **Start VPN client**:

   * On client: `/usr/local/bin/vpn-client -server <connect\_to\_ip>:443`
   * Waits a few seconds
   * Runs `route -n` on client to confirm `tun0` exists

8. **If `do\_test` is true**:

   * Runs `traceroute -n -m 5 8.8.8.8` from client:

     * Checks if `10.8.0.1` (VPN exit IP) appears as first hop

   * Runs `verify\_encryption`
   * For relay topology:

     * Reads `/tmp/vpn-h\_exit.log` and checks that:

       * The exit node sees `"New client connected: 10.0.0.2"` (relay IP) → PASS
       * If it ever sees `"New client connected: <client-IP>"` → FAIL

9. Starts Mininet CLI for interactive testing, then `net.stop()` on exit

**Required Functionality**:

* Provides a self-contained testbed for:

  * Single-hop VPN correctness
  * Multi-hop relay anonymity
  * Encryption vs plaintext leak detection

---

## 3\. Test Cases

### 3.1 Simple Topology Tests

**Setup**:

```bash
python topology.py
# or automated:
python topology.py --test
```

#### Test 1 — Client gets tunnel IP

**Command** (Mininet CLI):

```bash
mininet> h2 ip addr show tun0
```

**Expectation**:

* `tun0` is UP with an IP like `10.8.0.2/24`

---

#### Test 2 — Routing override (kill switch)

**Command**:

```bash
mininet> h2 route -n
```

**Expectation**:

* Routes `0.0.0.0 128.0.0.0` and `128.0.0.0 128.0.0.0` via `tun0`
* All default traffic goes via the VPN, not directly via `h2-eth0`

---

#### Test 3 — Traceroute through the tunnel

**Command**:

```bash
mininet> h2 traceroute -n 8.8.8.8
```

**Expectation**:

* Hop 1: `10.8.0.1` (VPN server's tunnel IP)
* Next hops: NAT and Docker gateways (e.g., `10.0.0.3`, `172.17.0.1`, etc.)

---

#### Test 4 — Internet reachability

**Command**:

```bash
mininet> h2 curl -I https://www.google.com
```

**Expectation**:

* `HTTP/2 200` or similar, confirming full internet connectivity through the VPN

---

#### Test 5 — Automated simple tests

**Command** (from container):

```bash
python topology.py --test
```

**Expectation**:

* Physical connectivity: 0% dropped pings
* Traceroute shows `10.8.0.1` as first hop
* Encryption test:

  * `\[+] PASS: No plaintext ICMP leaked`
  * `\[+] PASS: Encrypted TCP/443 traffic detected`

---

### 3.2 Relay Topology Tests

**Setup**:

```bash
python topology.py --topo relay --test
```

#### Test 1 — Basic connectivity

**Expectation**:

* 0% dropped (12/12 received) shows the network is wired correctly: `h\_client`, `h\_relay`, `h\_exit`, and NAT all reachable

---

#### Test 2 — Client tunnel IP

**Command**:

```bash
mininet> h\_client ip addr show tun0
```

**Expectation**:

* `10.8.0.2/24` on `tun0`, similar to simple topology

---

#### Test 3 — Routing through chain

**Result from script**:

* Traceroute from `h\_client`:

  * Hop 1: `10.8.0.1` (exit tunnel IP)
  * Later hops: `10.0.0.4`, `172.17.0.1`, etc.

* Script prints: `\[+] Traffic is routing through VPN`

---

#### Test 4 — Encryption / metadata removal

**Script internal**:

* `verify\_encryption` on `h\_client`:

  * No captured plain ICMP to the relay vs server IP
  * Captured TCP/443 to relay/server IP

**Expectation**:

* `\[+] PASS: No plaintext ICMP leaked`
* `\[+] PASS: Encrypted TCP/443 traffic detected`

---

#### Test 5 — Relay anonymity

**Script check**:

* Parses server (the exit node)'s `/tmp/vpn-h\_exit.log`

**Expectation**:

* `New client connected: 10.0.0.2` (relay IP)
* Not `New client connected: 10.0.0.3` (client IP)

**Result**:

```
\[+] PASS: Exit Node sees connection from Relay IP (10.0.0.2)
```

This confirms that:

* The exit node only sees the relay as the VPN client
* The original client's IP is hidden behind the relay

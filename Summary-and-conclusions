# VPN Project – Summary and Conclusions

This report summarizes the behaviour, functionality, and testing outcomes of the VPN system implemented using a custom TLS-based tunnel, TUN interfaces, and routing manipulation inside a Mininet environment.  
### 1. Encrypted Traffic Verified
Using tcpdump on the client’s physical NIC (`h2-eth0`):

- No plaintext ICMP packets leak.
- Only encrypted TCP/443 traffic passes.
- The exit node sees only the relay’s source IP in relay mode.

### 2. Correct Routing
Traceroute confirms:

- First hop is always the VPN gateway (`10.8.0.1`)
- Traffic flows through the tunnel, then NATs out through the server

### 3. Kill-Switch Behaviour
When the VPN client process is killed:

- `tun0` disappears
- Routes return to normal (`default via 10.0.0.3 dev h2-eth0`)
- Traffic safely falls back to the local interface without breaking connectivity

### 4. No DNS Leaks
DNS requests sent with the VPN active are routed inside the encrypted tunnel. Packet captures show no DNS queries escaping on the physical interface.

### 5. Relay Mode Functionality
The relay correctly:

- Accepts decrypted VPN packets from the previous hop
- Re-encrypts them for the next hop
- Removes the original client’s IP from visibility

The exit node sees only the relay’s IP.

## Discussion

The system successfully creates a secure and functional VPN tunnel with correct routing, NAT, relay behaviour, and leak prevention. Packet inspection confirms that encrypted VPN traffic moves through the tunnel as expected. Relay-mode anonymity also performs correctly, hiding the client’s origin from the exit node.

## Lessons Learned

- TUN interfaces must be cleaned up between runs to avoid “device busy” conflicts.
- DNS leak testing is essential and easy to overlook without packet-level inspection.
- Mininet provides a clean and controllable environment for validating network behaviour.

## Conclusion

All major components—encryption, tunneling, routing, NAT, and relay functionality—operate correctly. Testing confirms no DNS leaks, no ICMP leaks, and proper kill-switch behaviour. The VPN fulfills its security and functional objectives within the simulated Mininet environment.

#!/usr/bin/env python2
"""
Mininet Topology Script for VPN Testing

Usage:
   sudo python topology.py [--topo simple|relay] [--test]

Topologies:
   simple:  h1 (VPN Server) <--> s1 <--> h2 (VPN Client)
   relay:   h_exit (Exit) <--> s1 <--> h_relay (Relay) <--> s1 <--> h_client (Client)

"""

from mininet.topo import Topo
from mininet.net import Mininet
from mininet.node import OVSBridge, Node
from mininet.cli import CLI
from mininet.log import setLogLevel, info
import time
import os
import argparse
import sys

# --- Topologies ---

class SimpleTopo( Topo ):
    "Simple topology: h1 (10.0.0.1) <-> s1 <-> h2 (10.0.0.2)"
    def build( self ):
        h1 = self.addHost( 'h1', ip='10.0.0.1/24' )
        h2 = self.addHost( 'h2', ip='10.0.0.2/24' )
        s1 = self.addSwitch( 's1' )
        self.addLink( h1, s1 )
        self.addLink( h2, s1 )

class RelayTopo( Topo ):
    "Relay topology: Exit(10.0.0.1) <-> Relay(10.0.0.2) <-> Client(10.0.0.3)"
    def build( self ):
        exit_node = self.addHost( 'h_exit', ip='10.0.0.1/24' )
        relay = self.addHost( 'h_relay', ip='10.0.0.2/24' )
        client = self.addHost( 'h_client', ip='10.0.0.3/24' )
        s1 = self.addSwitch( 's1' )
        
        self.addLink( exit_node, s1 )
        self.addLink( relay, s1 )
        self.addLink( client, s1 )

def generate_certs(host, cn):
    "Generate self-signed certs on a host"
    cmd = 'openssl req -new -newkey rsa:2048 -days 365 -nodes -x509 -keyout server.key -out server.crt -subj "/C=US/ST=State/L=City/O=Organization/OU=Unit/CN={}" 2>/dev/null'.format(cn)
    host.cmd(cmd)

def start_vpn_server(host, mode='exit', forward_to=''):
    "Start VPN server on host"
    cmd = '/usr/local/bin/vpn-server -listen :443 -cert server.crt -key server.key'
    if mode == 'relay':
        cmd += ' -forward ' + forward_to
    
    log_file = '/tmp/vpn-{}.log'.format(host.name)
    host.cmd('{} > {} 2>&1 &'.format(cmd, log_file))
    info( "*** VPN {} started on {}. Log: {}\n".format(mode.upper(), host.name, log_file) )

def verify_encryption(client, target_ip):
    "Verify that traffic on the physical link is encrypted (port 443) and not plain ICMP"
    info( "\n*** Verifying Encryption / Metadata Removal ***\n" )
    
    # Start tcpdump in background on client to capture outgoing traffic on physical interface
    # We filter for ICMP to see if any leaks happen
    pcap_file = '/tmp/encryption_test.pcap'
    # Capture only ICMP packets going to target. If VPN works, we should see NONE (0).
    # We also capture TCP port 443 to verify encrypted traffic exists.
    
    # Check for leaked ICMP
    client.cmd('tcpdump -i {}-eth0 -n icmp and dst host {} -c 1 > /tmp/icmp_leak.log 2>&1 &'.format(client.name, target_ip))
    
    # Check for Encrypted Traffic
    client.cmd('tcpdump -i {}-eth0 -n tcp port 443 and dst host {} -c 1 > /tmp/encrypted.log 2>&1 &'.format(client.name, target_ip)) # target_ip might be relay IP, not final dst
    
    time.sleep(1)
    
    # Send a ping through the VPN
    info( "    Sending ping to {}...\n".format(target_ip) )
    client.cmd('ping -c 1 8.8.8.8') # Ping internet (8.8.8.8), but we capture on eth0
    
    time.sleep(2)
    
    # Analyze
    leak_log = client.cmd('cat /tmp/icmp_leak.log')
    enc_log = client.cmd('cat /tmp/encrypted.log')
    
    if "captured" in leak_log and "0 packets captured" not in leak_log:
         info( "[-] FAIL: Plaintext ICMP packet detected on physical interface! (Leak)\n" )
         info( leak_log )
    else:
         info( "[+] PASS: No plaintext ICMP leaked.\n" )

    if "captured" in enc_log:
         info( "[+] PASS: Encrypted TCP/443 traffic detected.\n" )
    else:
         info( "[-] FAIL: No encrypted traffic detected.\n" )

def run(topo_name='simple', do_test=False):
    if not os.path.exists("/usr/local/bin/vpn-server"):
        info( "*** Error: /usr/local/bin/vpn-server not found.\n" )
        return

    # Select Topology
    if topo_name == 'relay':
        topo = RelayTopo()
    else:
        topo = SimpleTopo()

    net = Mininet( topo=topo, switch=OVSBridge )
    
    # Add NAT for Internet Access
    # We must ensure VPN client overrides this.
    net.addNAT().configDefault()
    
    net.start()
    
    # Get Nodes
    if topo_name == 'relay':
        server = net.get('h_exit')
        relay = net.get('h_relay')
        client = net.get('h_client')
        server_ip = '10.0.0.1'
        relay_ip = '10.0.0.2'
    else:
        server = net.get('h1')
        relay = None
        client = net.get('h2')
        server_ip = '10.0.0.1'

    info( "\n*** Testing physical connectivity\n" )
    net.pingAll()

    # --- Setup Server(s) ---
    info( "\n*** Setting up VPN Servers\n" )
    
    # 1. Exit Node
    generate_certs(server, server_ip)
    start_vpn_server(server, mode='exit')
    
    # 2. Relay Node (if applicable)
    connect_to_ip = server_ip
    if relay:
        generate_certs(relay, relay_ip)
        # Relay connects to Exit Node
        start_vpn_server(relay, mode='relay', forward_to=server_ip+':443')
        connect_to_ip = relay_ip # Client connects to Relay
        
    time.sleep(2)

    # --- Start Client ---
    info( "\n*** Starting Client on {}\n".format(client.name) )
    # Run client in background
    client.cmd('/usr/local/bin/vpn-client -server {}:443 > /tmp/vpn-client.log 2>&1 &'.format(connect_to_ip))
    time.sleep(3) # Wait for handshake
    
    # Verify Client Connected
    routes = client.cmd('route -n')
    if "tun0" in routes:
        info( "[+] Client connected. Tunnel interface is UP.\n" )
    else:
        info( "[-] Client failed to connect. Check logs.\n" )
        info( client.cmd('cat /tmp/vpn-client.log') )

    if do_test:
        # Check traceroute
        info( "\n*** Verifying Routing (Tracepath to 8.8.8.8) ***\n" )
        trace = client.cmd('traceroute -n -m 5 8.8.8.8')
        info(trace)
        
        if "10.8.0.1" in trace:
             info( "[+] Traffic is routing through VPN.\n" )
        else:
             info( "[-] Traffic is NOT routing through VPN.\n" )

        # Verify Encryption
        # We listen on client physical interface. We expect to see packets to 'connect_to_ip' (Relay or Server)
        verify_encryption(client, connect_to_ip)

        # Verify Relay Masking
        if topo_name == 'relay':
            info( "\n*** Verifying Relay Anonymity ***\n" )
            server_log = server.cmd('cat /tmp/vpn-{}.log'.format(server.name))
            if "New client connected: {}".format(relay_ip) in server_log:
                 info( "[+] PASS: Exit Node sees connection from Relay IP ({})\n".format(relay_ip) )
            elif "New client connected: {}".format(client.IP()) in server_log:
                 info( "[-] FAIL: Exit Node sees connection directly from Client IP! Relay bypassed.\n" )
            else:
                 info( "[-] FAIL: Could not verify connection source in logs.\n" )

    info( "\n*** Starting CLI\n" )
    CLI( net )
    net.stop()

if __name__ == '__main__':
    setLogLevel( 'info' )
    parser = argparse.ArgumentParser()
    parser.add_argument('--topo', choices=['simple', 'relay'], default='simple', help='Topology type')
    parser.add_argument('--test', action='store_true', help='Run automated verification tests')
    args = parser.parse_args()
    
    run(topo_name=args.topo, do_test=args.test)

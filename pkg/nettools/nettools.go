package nettools

import (
	"fmt"
	"net"
	"os/exec"

	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

// CreateTUN creates a TUN interface with the given name and IP/CIDR.
// It returns the water interface (for reading/writing) and the netlink link (for config).
func CreateTUN(name string, ipCIDR string) (*water.Interface, netlink.Link, error) {
	config := water.Config{
		DeviceType: water.TUN,
	}
	config.PlatformSpecificParams = water.PlatformSpecificParams{
		Name: name,
	}

	ifce, err := water.New(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create TUN device: %v", err)
	}

	link, err := netlink.LinkByName(ifce.Name())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get link for %s: %v", ifce.Name(), err)
	}

	ip, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid IP/CIDR %s: %v", ipCIDR, err)
	}
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: ipNet.Mask,
		},
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return nil, nil, fmt.Errorf("failed to add address to link: %v", err)
	}

	if err := netlink.LinkSetMTU(link, 1300); err != nil {
		return nil, nil, fmt.Errorf("failed to set MTU: %v", err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return nil, nil, fmt.Errorf("failed to set link up: %v", err)
	}

	return ifce, link, nil
}

// EnableIPForwarding enables IPv4 forwarding via sysctl.
func EnableIPForwarding() error {
	cmd := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable ip forwarding: %v, output: %s", err, out)
	}
	return nil
}

// GetDefaultGateway returns the default gateway IP and the interface name it uses.
func GetDefaultGateway() (net.IP, string, error) {
	// Use 4 for AF_INET (IPv4)
	routes, err := netlink.RouteList(nil, 4)
	if err != nil {
		return nil, "", err
	}

	for _, route := range routes {
		if route.Dst == nil { // Default route has nil Dst or Dst.IP as 0.0.0.0/0
			link, err := netlink.LinkByIndex(route.LinkIndex)
			if err != nil {
				return nil, "", err
			}
			return route.Gw, link.Attrs().Name, nil
		}
	}
	return nil, "", fmt.Errorf("default gateway not found")
}

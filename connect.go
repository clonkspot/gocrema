package main

import (
	"fmt"
	"net"
	"time"

	"github.com/openclonk/netpuncher/c4netioudp"
)

var connectTimeout = 5 * time.Second

var privateIPBlocks []*net.IPNet

func init() {
	// conveniently stolen from stackoverflow
	// https://stackoverflow.com/questions/41240761/go-check-if-ip-address-is-in-private-network-space
	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addr
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Errorf("parse error on %q: %v", cidr, err))
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

// shouldSkipAddr checks for local addresses that should not be tested.
func shouldSkipAddr(addr net.Addr) bool {
	var ip net.IP
	switch a := addr.(type) {
	case *net.TCPAddr:
		ip = a.IP
	case *net.UDPAddr:
		ip = a.IP
	default:
		// unknown address type, skip
		return true
	}
	if !ip.IsGlobalUnicast() {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// tryConnect attempts to connect to the given address, returning true if the
// connection succeeds.
func tryConnect(addr net.Addr) bool {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return tryConnectTCP(a)
	case *net.UDPAddr:
		return tryConnectUDP(a)
	default:
		return false
	}
}

func tryConnectTCP(addr *net.TCPAddr) bool {
	conn, err := net.DialTimeout("tcp", addr.String(), connectTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func tryConnectUDP(addr *net.UDPAddr) bool {
	hdr := c4netioudp.PacketHdr{StatusByte: c4netioudp.IPID_Ping}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return false
	}
	hdr.WriteTo(conn)
	conn.SetReadDeadline(time.Now().Add(connectTimeout))
	buf := make([]byte, 1500)
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return false
	}
	// assume that the connection was successful if we received anything
	return n > 0
}

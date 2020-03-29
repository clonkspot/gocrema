package main

import (
	"fmt"
	"net"
	"time"

	"github.com/apex/log"
	"github.com/openclonk/netpuncher"
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
	case *NetpuncherAddr:
		return false
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
	case *NetpuncherAddr:
		return tryConnectNetpuncher(a)
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

// NetpuncherAddr is a net.Addr for a netpuncher connection.
type NetpuncherAddr struct {
	Net  string
	Addr string
	ID   uint64
}

// Network implements net.Addr
func (a *NetpuncherAddr) Network() string {
	return a.Net
}

func (a *NetpuncherAddr) String() string {
	return fmt.Sprintf("%s#%d", a.Addr, a.ID)
}

const (
	punchInterval = 100 * time.Millisecond
)

func tryConnectNetpuncher(a *NetpuncherAddr) bool {
	network := "udp"
	raddr, err := net.ResolveUDPAddr(network, a.Addr)
	if err != nil {
		log.WithError(err).WithField("addr", a.Addr).Errorf("tryConnectNetpuncher: invalid netpuncher address")
		return false
	}
	listener, err := c4netioudp.Listen(network, nil)
	if err != nil {
		log.WithError(err).Error("tryConnectNetpuncher: c4netioudp Listen failed")
		return false
	}
	defer listener.Close()

	conn, err := listener.Dial(raddr)
	if err != nil {
		log.WithError(err).Error("tryConnectNetpuncher: c4netioudp Dial failed")
		return false
	}
	defer conn.Close()

	// The following uses version 1 of the netpuncher protocol.
	header := netpuncher.Header{Version: 1}

	// Request punching for the given host id.
	sreq := netpuncher.SReq{Header: header, CID: uint32(a.ID)}
	b, err := sreq.MarshalBinary()
	if err != nil {
		log.WithError(err).Error("tryConnectNetpuncher: SReq.MarshalBinary failed")
		return false
	}
	conn.Write(b)
	log.WithField("packet", fmt.Sprintf("%+v", sreq)).Infof("tryConnectNetpuncher: -> %T", sreq)

	for {
		msg, err := netpuncher.ReadFrom(conn)
		if err != nil {
			log.WithError(err).Error("tryConnectNetpuncher: reading from netpuncher failed")
			return false
		}
		switch np := msg.(type) {
		case *netpuncher.AssID:
			log.Infof("tryConnectNetpuncher: CID = %d", np.CID)
		case *netpuncher.CReq:
			log.WithField("packet", fmt.Sprintf("%+v", msg)).Infof("tryConnectNetpuncher: <- %T", msg)
			// Try to establish communication.
			if err = listener.Punch(&np.Addr, connectTimeout, punchInterval); err != nil {
				log.WithError(err).WithField("raddr", np.Addr.String()).Error("tryConnectNetpuncher: punching failed")
				return false
			}
			// Punching success!
			return true
		default:
			log.WithField("packet", fmt.Sprintf("%+v", msg)).Infof("tryConnectNetpuncher: <- %T", msg)
		}
	}
}

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/golang/glog"
	flow "github.com/spyroot/rocinante/pkg/flow"
	"golang.org/x/sys/unix"
)

/**

 */
func main() {

	ipsrcstr := "127.0.0.1"
	ipdststr := "127.0.0.1"
	udpsrc := uint(10000)
	udpdst := uint(15000)
	showcsum := false

	flag.StringVar(&ipsrcstr, "ipsrc", ipsrcstr, "IPv4 source address")
	flag.StringVar(&ipdststr, "ipdst", ipdststr, "IPv4 destination address")
	flag.UintVar(&udpsrc, "udpsrc", udpsrc, "UDP source port")
	flag.UintVar(&udpdst, "udpdst", udpdst, "UDP destination port")
	flag.BoolVar(&showcsum, "showcsum", showcsum, "show checksums")
	flag.Parse()

	ipsrc := net.ParseIP(ipsrcstr)
	if ipsrc == nil {
		glog.Infof("invalid source IP: %v\n", ipsrc)
		os.Exit(1)
	}
	ipdst := net.ParseIP(ipdststr)
	if ipdst == nil {
		fmt.Fprintf(os.Stderr, "invalid destination IP: %v\n", ipdst)
		os.Exit(1)
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_RAW)

	if err != nil || fd < 0 {
		fmt.Fprintf(os.Stdout, "error creating a raw socket: %v\n", err)
		os.Exit(1)
	}

	err = unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_HDRINCL, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error enabling IP_HDRINCL: %v\n", err)
		unix.Close(fd)
		os.Exit(1)
	}

	ip := flow.Iphdr{
		Vhl:   0x45,
		Tos:   0,
		Id:    0x1234, // the kernel overwrites id if it is zero
		Off:   0,
		Ttl:   64,
		Proto: unix.IPPROTO_UDP,
	}
	copy(ip.Src[:], ipsrc.To4())
	copy(ip.Dst[:], ipdst.To4())
	// iplen and csum set later

	udp := flow.Udphdr{
		Src: uint16(udpsrc),
		Dst: uint16(udpdst),
	}
	// ulen and csum set later

	// just use an empty IPv4 sockaddr for Sendto
	// the kernel will route the packet based on the IP header
	addr := unix.SockaddrInet4{}

	for {
		stdin := bufio.NewReader(os.Stdin)
		line, err := stdin.ReadString('\n')
		if err != nil {
			glog.Errorf(err.Error())
			break
		}
		payload := []byte(line)
		udplen := 8 + len(payload)
		totalLen := 20 + udplen
		if totalLen > 0xffff {
			glog.Infof("message is too large to fit into a packet: %v > %v\n", totalLen, 0xffff)
			continue
		}

		// the kernel will overwrite the IP checksum, so this is included just for
		// completeness
		ip.Iplen = uint16(totalLen)
		ip.Checksum()

		// the kernel doesn't touch the UDP checksum, so we can either set it
		// correctly or leave it zero to indicate that we didn't use a checksum
		udp.Ulen = uint16(udplen)
		udp.Checksum(&ip, payload)
		if showcsum {
			glog.Infof("ip checksum: 0x%x, udp checksum: 0x%x\n", ip.Csum, udp.Csum)
		}

		var b bytes.Buffer
		err = binary.Write(&b, binary.BigEndian, &ip)
		if err != nil {
			glog.Info("error encoding the IP header: %v\n", err)
			continue
		}
		err = binary.Write(&b, binary.BigEndian, &udp)
		if err != nil {
			glog.Infof("error encoding the UDP header: %v\n", err)
			continue
		}
		err = binary.Write(&b, binary.BigEndian, &payload)
		if err != nil {
			glog.Infof("error encoding the payload: %v\n", err)
			continue
		}
		bb := b.Bytes()

		/*
		 * For some reason, the IP header's length field needs to be in host byte order
		 * in OS X.
		 */
		if runtime.GOOS == "darwin" {
			bb[2], bb[3] = bb[3], bb[2]
		}

		err = unix.Sendto(fd, bb, 0, &addr)
		if err != nil {
			glog.Error("error sending the packet: %v\n", err)
			continue
		}
		fmt.Printf("%v bytes were sent\n", len(bb))
	}

	err = unix.Close(fd)
	if err != nil {
		glog.Infof("error closing the socket: %v\n", err)
		os.Exit(1)
	}
}

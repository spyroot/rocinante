package flow

import (
	"bytes"
	"encoding/binary"
)

type Hdr struct {
	SrcPort string `json:"src port"`
	DstPort string `json:"dst port"`
	SrcIp   string `json:"src ip"`
	DstIp   string `json:"dst ip"`
	Proto   string `json:"protocol"`
}

type Iphdr struct {
	Vhl   uint8
	Tos   uint8
	Iplen uint16
	Id    uint16
	Off   uint16
	Ttl   uint8
	Proto uint8
	Csum  uint16
	Src   [4]byte
	Dst   [4]byte
}

func (h *Iphdr) Checksum() {
	h.Csum = 0
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, h)
	h.Csum = checksum(b.Bytes())
}

func checksum(buf []byte) uint16 {
	sum := uint32(0)
	for ; len(buf) >= 2; buf = buf[2:] {
		sum += uint32(buf[0])<<8 | uint32(buf[1])
	}
	if len(buf) > 0 {
		sum += uint32(buf[0]) << 8
	}
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	csum := ^uint16(sum)
	if csum == 0 {
		csum = 0xffff
	}
	return csum
}

type Udphdr struct {
	Src  uint16
	Dst  uint16
	Ulen uint16
	Csum uint16
}

func (u *Udphdr) Checksum(ip *Iphdr, payload []byte) {
	u.Csum = 0
	phdr := Pseudohdr{
		Ipsrc:   ip.Src,
		Ipdst:   ip.Dst,
		Zero:    0,
		Ipproto: ip.Proto,
		Plen:    u.Ulen,
	}
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, &phdr)
	_ = binary.Write(&b, binary.BigEndian, u)
	_ = binary.Write(&b, binary.BigEndian, &payload)
	u.Csum = checksum(b.Bytes())
}

// pseudo header used for checksum calculation
type Pseudohdr struct {
	Ipsrc   [4]byte
	Ipdst   [4]byte
	Zero    uint8
	Ipproto uint8
	Plen    uint16
}

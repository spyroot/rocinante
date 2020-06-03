/**
    Bunch of hash utils used in rocinante

	Mustafa Bayramov
 */
package hash

import (
	"encoding/binary"
	"hash/fnv"
	"net"
)

/*
	take string return 64 bit hash
 */
func Hash64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

/*
	Take string return 64 bit hash
*/
func HashAddr(addr net.Addr) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(addr.String()))
	return h.Sum64()
}

/*
	Take string return 64 bit hash
*/
func HashAddrPort(addr net.IP, srcPort uint32) uint64 {

	h := fnv.New64a()
	_, _ = h.Write([]byte(addr.String()))

	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, srcPort)
	_, _ = h.Write(p)

	return h.Sum64()
}


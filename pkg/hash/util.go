/**
    Bunch of hash utils used in rocinante

	Mustafa Bayramov
*/
package hash

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"hash/fnv"
	"net"
)

func GenericHash(s interface{}) []byte {
	var b bytes.Buffer
	gob.NewEncoder(&b).Encode(s)
	return b.Bytes()
}

func AsSha256(o interface{}) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%v", o)))

	return fmt.Sprintf("%x", h.Sum(nil))
}

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

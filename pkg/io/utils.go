package io

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
)

/*
	Return if given IP:PORT bind open or not, network must
    must be "tcp", "tcp4", "tcp6", "unix" or "unixpacket".

	No validation, caller must do that
*/
func CheckSocket(hostPort string, proto string) bool {

	l, err := net.Listen(proto, hostPort)
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return false
	}
	return true
}

/**
Generate id
*/
func GenerateId(address string, port string) string {
	return address + ":" + port
}

/**

 */
func ReadUint64(data []byte) (ret uint64) {
	buf := bytes.NewBuffer(data)
	binary.Read(buf, binary.LittleEndian, &ret)
	return
}

/**

 */
func IsDir(dir string) (bool, error) {

	//
	src, err := os.Stat(dir)
	if err != nil {
		return false, err
	}
	// check if the source is indeed a directory or not
	if !src.IsDir() {
		return false, err
	}
	return true, nil
}

package server

import "net"

//
func CheckSocket(hostPort string) bool {

	l, err := net.Listen("tcp", hostPort)
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return false
	}
	return true
}

//
func GenerateId(address string, port string) string {
	return address + ":" +port
}
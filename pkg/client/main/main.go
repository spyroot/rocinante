package main

import (
	"../../client"
	"github.com/apex/log"
	"github.com/golang/glog"
)

const (
	address     = "localhost:50051"
	defaultName = "world"
)

func main() {
	// Set up a connection to the server.
	apiClient, err := client.NewRestClientFromUrl("http://localhost:8001")
	if err != nil {
		log.Fatal(err.Error())
		return
	}

	peerStatus, err := apiClient.GetPeerList()
	if err != nil {
		log.Fatal(err.Error())
		return
	}

	for _, p := range peerStatus {
		glog.Infof("%v", p)
	}
}

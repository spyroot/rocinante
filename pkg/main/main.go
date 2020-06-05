package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	hs "../hash"
	"../server"
	"../server/artifacts"
	"github.com/golang/glog"
)

//
//var ports = map[int]string{
//	0: ":50051",
//	1: ":50052",
//	2: ":50053",
//}

// return if a tcp/udp port is free to use
//

// generate server id
func generateId(address string, port string) string {
	return address + ":" + port
}

// usage for rocinante
func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "usage: example -stderrthreshold=[INFO|WARNING|FATAL] -log_dir=[string]\n")
	flag.PrintDefaults()
	os.Exit(2)
}

//
func init() {
	flag.Usage = usage
	_ = flag.Set("logtostderr", "true")
	_ = flag.Set("stderrthreshold", "WARNING")
	_ = flag.Set("v", "2")
	flag.Parse()
}

func main() {

	var configFile = "config.yaml"

	if len(os.Args) > 1 {
		argList := os.Args[1:]
		configFile = argList[0]
	}

	artifact, err := artifacts.Read(configFile)
	if err != nil {
		log.Fatal(err)
	}

	if len(artifact.Formation.Cluster.Controllers) == 0 {
		log.Fatal("Empty controller list")
	}

	controllers := artifact.Formation.Cluster.Controllers
	_, err = net.ResolveTCPAddr("tcp", "0.0.0.0:12345")
	if err != nil {
		log.Fatal(err)
	}

	networkSpec := make([]server.ServerSpec, 0)

	var myPort string
	var raftBinding string
	var restBinding string
	var myNetworkSpec server.ServerSpec

	for i, controller := range controllers {

		raftBinding = generateId(controllers[i].Address, controller.Port)
		restBinding = generateId(controllers[i].Address, controller.Rest)

		myNetworkSpec = server.ServerSpec{
			ServerID:        hs.Hash64(raftBinding),
			RaftNetworkBind: raftBinding,
			RestNetworkBind: restBinding,
			GrpcNetworkBind: raftBinding,
			Basedir:         artifact.BaseDir,
			LogDir:          "",
		}

		if server.CheckSocket(raftBinding) && server.CheckSocket(restBinding) {
			glog.Infof("Found unused port, server id %s", raftBinding)
			myPort = controllers[i].Port
			for p := 0; p < len(controllers); p++ {
				if p != i {
					raftBind := generateId(controllers[p].Address, controllers[p].Port)
					restBind := generateId(controllers[p].Address, controllers[p].Rest)

					spec := server.ServerSpec{
						RaftNetworkBind: raftBind,
						RestNetworkBind: restBind,
						GrpcNetworkBind: raftBind,
					}
					networkSpec = append(networkSpec, spec)
				}
			}
			break
		}
	}

	if len(networkSpec) > len(controllers) {
		log.Fatal("Error number of peer can't > than number of node in cluster.")
	}

	if len(raftBinding) == 0 {
		glog.Fatal("Can't find free port. All port in use. ")
	}

	glog.Infof("Starting server on a port [%s]", myPort)
	ready := make(chan interface{})
	ns, err := server.NewServer(myNetworkSpec, networkSpec, myPort, ready)
	if err != nil {
		glog.Fatal("Failed to start server", err)
	}

	close(ready)
	err = ns.Serve()
	if err != nil {
		glog.Error(err)
	}
	glog.Flush()
}

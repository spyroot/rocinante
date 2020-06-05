package server

import (
	"../server/artifacts"
	"fmt"
	"github.com/golang/glog"
	"net"
	"sync"
	"testing"
)

/*
   Setup n number server.
   sample artifact

artifact:
  cleanupOnFailure: true
  cluster:
    name: test
    controllers:
      - address: 127.0.0.1
        port: 35001
        rest: 8001
        wwwroot: /Users/spyroot/go/src/github.com/spyroot/rocinante/pkg/template/
      - address: 127.0.0.1
        port: 35002
        rest: 8002
        wwwroot: /Users/spyroot/go/src/github.com/spyroot/rocinante/pkg/template/
      - address: 127.0.0.1
        port: 35003
        rest: 8003
        wwwroot: /Users/spyroot/go/src/github.com/spyroot/rocinante/pkg/template/
  global:
*/
func SetupTestCase(t *testing.T, config string, quit chan interface{}) (func(t *testing.T), []*Server, error) {

	// lock used to protect servers return value ,
	// while we initialize in go routine each server
	var lck = &sync.Mutex{}
	var servers []*Server

	t.Log("setup test case")
	var configFile = config
	artifact, err := artifacts.Read(configFile)
	if err != nil {
		return nil, nil, err
	}
	if len(artifact.Formation.Cluster.Controllers) == 0 {
		return nil, nil, fmt.Errorf("empty controller list")
	}
	controllers := artifact.Formation.Cluster.Controllers
	_, err = net.ResolveTCPAddr("tcp", "0.0.0.0:12345")
	if err != nil {
		return nil, nil, err
	}

	t.Log("setup server")

	var wg sync.WaitGroup

	for i, controller := range controllers {
		networkSpec := make([]ServerSpec, 0)

		myID := GenerateId(controllers[i].Address, controller.Port)
		myRest := GenerateId(controllers[i].Address, controller.Rest)

		myNetworkSpec := ServerSpec{
			hash(myID),
			myRest,
			"",
			artifact.BaseDir,
			"",
			"",
		}

		// if both port area free add to peer list, all other peers
		// final result should:
		// myNetworkSpec hold server spec
		// peerSpec hold all other peer spec
		if CheckSocket(myID) && CheckSocket(myRest) {
			glog.Infof("Found unused port, server id ", myID)
			myPort := controllers[i].Port
			for p := 0; p < len(controllers); p++ {
				if p != i {
					raftBind := GenerateId(controllers[p].Address, controllers[p].Port)
					restBind := GenerateId(controllers[p].Address, controllers[p].Rest)
					spec := ServerSpec{
						RaftNetworkBind: raftBind,
						RestNetworkBind: restBind,
						GrpcNetworkBind: "",
					}
					networkSpec = append(networkSpec, spec)
				}
			}

			wg.Add(1)
			go func(mySpec ServerSpec, peerSpec []ServerSpec, p string) {

				// start serving
				t.Log("Starting server", mySpec)
				ready := make(chan interface{})
				srv, err := NewServer(mySpec, peerSpec, p, ready)
				if err != nil {
					glog.Fatal("Failed to start server. %v", err)
				}

				lck.Lock()
				servers = append(servers, srv)
				t.Log("Added server to a list", len(servers))
				wg.Done()
				lck.Unlock()

				close(ready)
				err = srv.Serve()
				if err != nil {
					glog.Error(err)
				}
			}(myNetworkSpec, networkSpec, myPort)
		}
		t.Log("server started.")
	}

	wg.Wait()
	t.Log("all server started.")

	// return callback to close channel
	return func(t *testing.T) {
		t.Log("Shutdown.")
		close(quit)
		t.Log("teardown test case")
	}, servers, nil
}

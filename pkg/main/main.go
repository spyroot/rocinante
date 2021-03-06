package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spyroot/rocinante/pkg/color"
	hs "github.com/spyroot/rocinante/pkg/hash"
	"github.com/spyroot/rocinante/pkg/io"
	"github.com/spyroot/rocinante/pkg/server"
	"github.com/spyroot/rocinante/pkg/server/artifacts"

	"github.com/golang/glog"
)

// generate server id
func generateId(address string, port string) string {
	return address + ":" + port
}

//logtostderr (bool, default=false)
//Log messages to stderr instead of logfiles.
//Note: you can set binary flags to true by specifying 1, true, or yes (case insensitive). Also, you can set binary flags to false by specifying 0, false, or no (again, case insensitive).
//stderrthreshold (int, default=2, which is ERROR)
//Copy log messages at or above this level to stderr in addition to logfiles. The numbers of severity levels INFO, WARNING, ERROR, and FATAL are 0, 1, 2, and 3, respectively.
//minloglevel (int, default=0, which is INFO)
//Log messages at or above this level. Again, the numbers of severity levels INFO, WARNING, ERROR, and FATAL are 0, 1, 2, and 3, respectively.
//log_dir (string, default="")
//If specified, logfiles are written into this directory instead of the default logging directory.
//v (int, default=0)
//Show all VLOG(m) messages for m less or equal the value of this flag. Overridable by --vmodule. See the section about verbose logging for more detail.
//vmodule (string, default="")
//Per-module verbose level. The argument has to contain a comma-separated list of <module name>=<log level>. <module name> is a glob pattern (e.g., gfs* for all modules whose name starts with "gfs"), matched against the filename base (that is, name ignoring .cc/.h./-inl.h). <log level> overrides any value given by --v. See also the section about verbose logging.
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

/**
Shutdown handler to shutdown server
*/
func signalHandler(server *server.Server) {
	msg := color.Red + "shutting down server" + color.Reset
	glog.Infof(msg)
	if server != nil {
		server.Shutdown()
	}
}

/**

 */
func main() {

	// default file
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
			MetricBind:      controllers[i].Metric,
			Basedir:         artifact.BaseDir,
			LogDir:          "",
		}

		if io.CheckSocket(raftBinding, "tcp") && io.CheckSocket(restBinding, "tcp") {
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
						MetricBind:      controllers[i].Metric,
					}
					networkSpec = append(networkSpec, spec)
				}
			}
			break
		}
	}

	glog.Infof(myNetworkSpec.Basedir)
	if len(networkSpec) > len(controllers) {
		log.Fatal("Error number of peer can't > than number of node in cluster.")
		return
	}
	if len(raftBinding) == 0 {
		glog.Fatal("Can't find free port. All port in use.")
		return
	}

	glog.Infof("Starting server on a port [%s]", myPort)
	ready := make(chan interface{})
	localServer, err := server.NewServer(myNetworkSpec, networkSpec, myPort, true, ready)
	if err != nil {
		glog.Fatal("Failed to start server", err)
	}

	if localServer == nil {
		log.Fatal("Failed create a server.")
		os.Exit(1)
	}
	// add signal handler , on stop shutdown
	signalChannel := make(chan os.Signal)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChannel
		signalHandler(localServer)
		os.Exit(1)
	}()

	close(ready)
	err = localServer.Serve()
	if err != nil {
		glog.Error(err)
	}

	glog.Flush()
}

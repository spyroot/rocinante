package main

import (
	"flag"
	"fmt"
	"os"

	"../../loadbalancer"
	"../artifacts"
	"github.com/golang/glog"
)

// usage for load balancer in rocinante
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
		glog.Fatal(err)
	}

	if len(artifact.Service.Pool.Servers) == 0 {
		glog.Fatal("Server pool contain no server. Check configuration list.")
	}

	servers := artifact.Service.Pool.Servers
	api := artifact.Service.Pool.Api
	var farm []string
	for _, s := range servers {
		s := "http://" + s.Address + ":" + s.Port
		farm = append(farm, s)
	}

	var apiRestEndpoints []string
	for _, s := range api {
		s := "http://" + s.Address + ":" + s.Rest
		apiRestEndpoints = append(apiRestEndpoints, s)
	}

	//func NewLoadBalance(port int, srv [] string, retry int, ptype ProbeMethod, probeDuration time.Duration, method Algorithm) *LoadBalancer {
	loadBalance, _ := loadbalancer.NewLoadBalance(9000, farm, 3, loadbalancer.ProbeHttp, 2, loadbalancer.SourceHash, apiRestEndpoints)
	loadBalance.Serve()

}

/**
    Simple http load balancer implementation with
    synchronization a state in rocinante.

	Mustafa Bayramov
*/
package loadbalancer

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"

	"../client"
	"github.com/golang/glog"
)

type ProbeMethod int
type Algorithm int
type Status int

const (
	ProbeIcmp ProbeMethod = iota
	ProbeHttp
)

const (
	RoundRobin Algorithm = iota
	SourceHash
)

const (
	Attempts Status = iota
	Retry
)

type LoadBalancer struct {

	// mutex for a load balancer server
	lock sync.RWMutex

	// server pools
	servers    []string
	serverPool ServerPool

	// server status
	isDead bool
	// service port
	port int
	//
	maxRetry int
	//
	healthTime time.Duration
	// type of probing
	probeType ProbeMethod
	// algorithm and method load balance for a give service
	method Algorithm
	// cluster api end point, we will use this to send server status, load balancer hash selection etc.
	apiEndpoits []string
	// rest client
	apiClient *client.RestClient
}

/**

 */
func (lb *LoadBalancer) attemptHandler(r *http.Request) int {
	if a, ok := r.Context().Value(Attempts).(int); ok {
		return a
	}
	return 1
}

/*

 */
func (lb *LoadBalancer) retryHandler(r *http.Request) int {
	if r, ok := r.Context().Value(Retry).(int); ok {
		return r
	}
	return 0
}

/**

 */
func getAddress(req *http.Request) (*net.IP, string, error) {

	ip, port, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return nil, "", err
	}

	addr := net.ParseIP(ip)
	if addr == nil {
		return nil, "", err
	}

	return &addr, port, nil
}

/**

 */
func (lb *LoadBalancer) sourceHashHandler(w http.ResponseWriter, r *http.Request) *ServerFarm {

	addr, port, err := getAddress(r)
	if err != nil {
		http.Error(w, "Service not available.", http.StatusServiceUnavailable)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		http.Error(w, "Service not available.", http.StatusServiceUnavailable)
	}
	s, err := lb.serverPool.HashSelection(*addr, uint32(portNum))
	if err != nil {
		http.Error(w, "Service not available.", http.StatusServiceUnavailable)
	}

	glog.Infof("remote address %+v", r.RemoteAddr)

	// do lookup based on remote address
	if lb.apiClient != nil {
		resp, httpErr := lb.apiClient.Get(r.RemoteAddr)
		if httpErr == nil && resp.Success == true {
			glog.Infof("got hit")
		}
	}

	// we don't wait respond
	go func(s *ServerFarm, api *client.RestClient) {
		if api != nil && s != nil {
			ok, err := api.Store(r.RemoteAddr, []byte(s.URL.Host))
			if err != nil {
				glog.Error(err.Error())
			}
			if ok == false {
				glog.Error("failed to store")
			}
		}
	}(s, lb.apiClient)

	return s
}

/**

 */
func (lb *LoadBalancer) httpHandler(w http.ResponseWriter, r *http.Request) {

	attempts := lb.attemptHandler(r)
	if attempts > 3 {
		glog.Infof("%s(%s) Max attempts reached, terminating\n", r.RemoteAddr, r.URL.Path)
		http.Error(w, "Service not available", http.StatusServiceUnavailable)
		return
	}

	// algorithm we use
	var srv *ServerFarm
	if lb.method == SourceHash {
		srv = lb.sourceHashHandler(w, r)
	} else {
		// default method round robin
		s, err := lb.serverPool.RoundRobin()
		if err != nil {
			http.Error(w, "Service not available.", http.StatusServiceUnavailable)
		}
		srv = s
	}

	// if we found a server in pool, connect to it via reverse
	if srv != nil {
		srv.ReverseProxy.ServeHTTP(w, r)
		return
	}

	http.Error(w, "Service not available.", http.StatusServiceUnavailable)
}

/**

 */
func isBackendAlive(u *url.URL) bool {
	conn, err := net.DialTimeout("tcp", u.Host, 2*time.Second)
	if err != nil {
		log.Println("Site unreachable, error: ", err)
		return false
	}
	_ = conn.Close()
	return true
}

/**
	handler for health check, we should run in
    separate go routine
*/
func (lb *LoadBalancer) healthCheck() {

	ticket := time.NewTicker(time.Minute * lb.healthTime)
	for {
		select {
		case <-ticket.C:
			glog.Infof("Starting health check...")
			lb.serverPool.HealthCheck()
		}
	}
}

/**
	Return instance of load balancer

 	port that load balancer list on public side
    srv - server farm behind load balancer
    retry - number of retry before we declare server dead
    probeType - how load balancer need to probe each server
    proobeTime -
*/
func NewLoadBalance(port int, srv []string, retry int, ptype ProbeMethod,
	probeDuration time.Duration, method Algorithm, api []string) (*LoadBalancer, error) {

	s := new(LoadBalancer)

	if len(srv) == 0 {
		return nil, fmt.Errorf("sever pool is empty")
	}

	s.isDead = false
	s.servers = srv
	s.maxRetry = retry
	s.probeType = ptype
	s.healthTime = probeDuration
	s.port = port
	s.method = method
	s.apiEndpoits = api

	var err error
	s.apiClient, err = client.NewRestClient(api)
	if err != nil {
		return s, nil
	}

	return s, nil
}

/**

 */
func (lb *LoadBalancer) addServer(srv string) {
	lb.servers = append(lb.servers, srv)
	lb.buildServerPools()
}

/*

 */
func (lb *LoadBalancer) SetupHealthCheck() {
	go lb.healthCheck()
}

/*

 */
func (lb *LoadBalancer) StartLoadBalancer() {

	// create http server
	server := http.Server{
		Addr:    fmt.Sprintf(":%d", lb.port),
		Handler: http.HandlerFunc(lb.httpHandler),
	}

	glog.Infof("Load Balancer started at :%d\n", lb.port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

/**
  Handler for http protocol
*/
func (lb *LoadBalancer) httpLoadBalancerHandler(serverUrl *url.URL) *httputil.ReverseProxy {

	// build connect to a target server
	proxy := httputil.NewSingleHostReverseProxy(serverUrl)
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, e error) {
		glog.Infof("[%s] %s\n", serverUrl.Host, e.Error())
		retries := lb.retryHandler(request)

		if retries < lb.maxRetry {
			select {
			case <-time.After(10 * time.Millisecond):
				// we send request with context
				ctx := context.WithValue(request.Context(), Retry, retries+1)
				proxy.ServeHTTP(writer, request.WithContext(ctx))
			}
			return
		}

		lb.serverPool.UpdateMemberStatus(serverUrl, false)
		attempts := lb.attemptHandler(request)

		log.Printf("%s(%s) Attempting retry %d\n", request.RemoteAddr, request.URL.Path, attempts)
		ctx := context.WithValue(request.Context(), Attempts, attempts+1)
		lb.httpHandler(writer, request.WithContext(ctx))
	}

	return proxy
}

/**
Build list of server for a pool
*/
func (lb *LoadBalancer) buildServerPools() {

	for _, server := range lb.servers {
		serverUrl, err := url.Parse(server)
		if err != nil {
			glog.Infof("can't parse [%s] server to url.", server)
			continue
		}

		httpProxy := lb.httpLoadBalancerHandler(serverUrl)
		lb.serverPool.Add(&ServerFarm{
			URL:          serverUrl,
			Alive:        true,
			ReverseProxy: httpProxy,
		})
	}
}

/*

 */
func (lb *LoadBalancer) Serve() {
	lb.buildServerPools()
	lb.SetupHealthCheck()
	lb.StartLoadBalancer()
}

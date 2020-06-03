/**

  Server pool selection
  It support round robin and weight selection.

  Mustafa Bayramov
 */
package loadbalancer

import (
	"fmt"
	"net"
	"net/url"
	"sync"
	"sync/atomic"

	"../client"
	hs "../hash"
	"github.com/golang/glog"
)

type ServerPool struct {

	lock     sync.Mutex
	members  []*ServerFarm
	current  uint64
	poolSize uint64

	api		 *client.RestClient
}

/**
	Source hash selection.

	Hash source (address, port) mod num_server_alive

 */
func (s *ServerPool) HashSelection(addr net.IP, srcPort uint32) (*ServerFarm, error) {

	var numAlive = 0
	for i := 0; i < int(s.poolSize); i++ {
		if s.members[i].IsAlive() {
			numAlive++
		}
	}
	if numAlive == 0 {
		return nil,  fmt.Errorf("all server are dead")
	}

	// hash
	hash := hs.HashAddrPort(addr, srcPort) %  uint64(numAlive)
	glog.Infof("Server hash id %d,  serialize hash to cluster", hash)
	return s.members[hash], nil
}


/*
     Adds server to a pool
 */
func (s *ServerPool) Add(backend *ServerFarm) {
	if s != nil {
		s.lock.Lock()
		defer s.lock.Unlock()
		s.members = append(s.members, backend)
		s.poolSize = uint64(len(s.members))

	}
}

/**
   Return next server from a pool
 */
func (s *ServerPool) Next() uint64 {
	if s == nil {
		return 0
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	return atomic.AddUint64(&s.current, 1) % s.poolSize
}

/*
	Iterate over server pool ad adjust status.
 */
func (s *ServerPool) UpdateMemberStatus(backendUrl *url.URL, alive bool) {

	s.lock.Lock()
	defer s.lock.Unlock()

	for _, b := range s.members {
		if b.URL.String() == backendUrl.String() {
			b.SetAlive(alive)
			break
		}
	}
}

/*
   Select server based on modulo operation in round robin
 */
func (s *ServerPool) RoundRobin() (*ServerFarm,error) {

	if s == nil {
		return nil, fmt.Errorf("server pool is nil")
	}

	next := s.Next()
	l := s.poolSize + next
	for i := next; i < l; i++ {
		serverID := i % s.poolSize
		if s.members[serverID].IsAlive() {
			if i != next {
				atomic.StoreUint64(&s.current, serverID)
			}
			return s.members[serverID], nil
		}
	}
	return nil,  fmt.Errorf("all server are dead")
}

/**
	For each member iterate and check health of the server
 */
func (s *ServerPool) HealthCheck() {

	s.lock.Lock()
	defer s.lock.Unlock()

	for _, s := range s.members {
		alive := isBackendAlive(s.URL)
		s.SetAlive(alive)
		glog.Infof("%s is alive [%v]\n", s.URL, alive)
	}
}

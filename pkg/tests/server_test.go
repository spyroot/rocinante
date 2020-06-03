/***

	on going work.
   Mustafa Bayramov
*/
package tests

import (
	"flag"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"../server"
)

// return if a tcp/udp port is free to use
func checkSocket(hostPort string) bool {

	l, err := net.Listen("tcp", hostPort)
	if l != nil {
		defer l.Close()
	}
	if err != nil {
		return false
	}
	return true
}

// generate server id
func generateId(address string, port string) string {
	return address + ":" + port
}

//	mainly to make glog happy
func usage() {
}

/**
mainly to make glog happy
*/
func init() {
	log.Printf("test")
	flag.Usage = usage
	flag.Set("test.v", "true")

}

/**
Simple leader election.
*/
func TestLeaderElection(t *testing.T) {

	type args struct {
		serverSpec server.ServerSpec
		peers      []server.ServerSpec
		port       string
		ready      <-chan interface{}
	}
	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic leader election",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			wantErr: true,
		},
	}

	// log.SetOutput(ioutil.Discard)
	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, "/Users/spyroot/go/src/github.com/spyroot/rocinante/config.yaml", quit)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	t.Log("Number of servers", len(servers))

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var converged bool = false
			var current_leader uint64 = 0
			// repeat tt.repeat times
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				defer ticker.Stop()
				<-ticker.C

				for _, s := range servers {
					leader_id, _, isLeader := s.RaftState().Status()
					t.Log("node stats", leader_id, isLeader)
					if isLeader == true {
						t.Log("leader elected", s.LeaderId)
						current_leader = leader_id
						converged = true
						break
					}
				}
			}

			// otherwise passed
			if current_leader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", current_leader)
			}

			// we check that only one server is leader
			numLeaders := 0
			var convergedTerm uint64 = 0
			for _, s := range servers {
				_, term, isLeader := s.RaftState().Status()
				if isLeader == true {
					numLeaders++
				}

				if convergedTerm == 0 {
					convergedTerm = term
				} else {
					if convergedTerm != term {
						t.Errorf("expected same term in all cluster member.")
					}
				}
			}

			if numLeaders > 1 {
				t.Errorf("expected single leader in the cluster")
			}

			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	t.Log("Shutdown all servers.")
	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}
}

/**
    Test start 3 server,  fail one and check re-election.

	Note if run all test at same time,  TCP stack need release all ports.

*/
func TestLeaderElection2(t *testing.T) {

	type args struct {
		serverSpec server.ServerSpec
		peers      []server.ServerSpec
		port       string
		ready      <-chan interface{}
	}
	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic leader election",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			wantErr: true,
		},
	}

	// log.SetOutput(ioutil.Discard)
	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, "/Users/spyroot/go/src/github.com/spyroot/rocinante/config.yaml", quit)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	time.Sleep(2 * time.Second)

	t.Log("Number of servers", len(servers))

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var converged bool = false
			var currentLeader uint64 = 0
			// repeat tt.repeat times
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				defer ticker.Stop()
				<-ticker.C

				for _, s := range servers {
					leader_id, _, isLeader := s.RaftState().Status()
					t.Log("node stats", leader_id, isLeader)
					if isLeader == true {
						t.Log("leader elected", leader_id)
						currentLeader = leader_id
						converged = true
						break
					}
				}
			}

			// otherwise passed
			if currentLeader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", currentLeader)
			}

			// we check that only one server is leader
			numLeaders := 0
			var convergedTerm uint64 = 0
			for _, s := range servers {
				_, term, isLeader := s.RaftState().Status()
				if isLeader == true {
					numLeaders++
				}

				if convergedTerm == 0 {
					convergedTerm = term
				} else {
					if convergedTerm != term {
						t.Errorf("expected same term in all cluster member.")
					}
				}
			}

			if numLeaders > 1 {
				t.Errorf("expected single leader in the cluster")
			}

			// re-election
			for _, s := range servers {
				_, _, isLeader := s.RaftState().Status()
				// shutdown.
				if isLeader {
					s.Shutdown()
				}
			}

			// repeat,  we should have two server and leader re-elected
			//converged bool = false
			//current_leader uint64 = 0
			// repeat tt.repeat times
			oldLeader := currentLeader
			currentLeader = 0
			converged = false
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				defer ticker.Stop()
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if isLeader == true {
						t.Log("new leader elected", leaderId, oldLeader)
						currentLeader = leaderId
						converged = true
						break
					}
				}
			}

			// otherwise passed
			if currentLeader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", currentLeader)
			}

			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	t.Log("Shutdown all servers.")
	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}
}

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestLeaderElection3(t *testing.T) {

	type args struct {
		serverSpec server.ServerSpec
		peers      []server.ServerSpec
		port       string
		ready      <-chan interface{}
	}
	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic leader election",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			wantErr: true,
		},
	}

	time.Sleep(2 * time.Second)
	// log.SetOutput(ioutil.Discard)
	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, "/Users/spyroot/go/src/github.com/spyroot/rocinante/config.yaml", quit)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	t.Log("Number of servers", len(servers))

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var converged bool = false
			var current_leader uint64 = 0
			// repeat tt.repeat times
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				defer ticker.Stop()
				<-ticker.C

				for _, s := range servers {
					leader_id, _, isLeader := s.RaftState().Status()
					t.Log("node stats", leader_id, isLeader)
					if isLeader == true {
						t.Log("leader elected", leader_id)
						current_leader = leader_id
						converged = true
						break
					}
				}
			}

			// otherwise passed
			if current_leader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", current_leader)
			}

			// we check that only one server is leader
			numLeaders := 0
			var convergedTerm uint64 = 0
			for _, s := range servers {
				_, term, isLeader := s.RaftState().Status()
				if isLeader == true {
					numLeaders++
				}

				if convergedTerm == 0 {
					convergedTerm = term
				} else {
					if convergedTerm != term {
						t.Errorf("expected same term in all cluster member.")
					}
				}
			}

			if numLeaders > 1 {
				t.Errorf("expected single leader in the cluster")
			}

			/************* re-election   **************/
			for _, s := range servers {
				_, _, isLeader := s.RaftState().Status()
				// shutdown.
				if isLeader {
					s.Shutdown()
				}
			}

			// repeat,  we should have two server and leader re-elected
			// converged bool = false
			// current_leader uint64 = 0
			// repeat tt.repeat times
			oldLeader := current_leader
			current_leader = 0
			converged = false
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				defer ticker.Stop()
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if isLeader == true {
						t.Log("new leader elected", s.LeaderId, oldLeader)
						current_leader = leaderId
						converged = true
						break
					}
				}
			}

			// otherwise passed
			if current_leader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", current_leader)
			}

			/** waiting and re add server back to a cluster */
			time.Sleep(2 * time.Second)
			for _, s := range servers {
				if s.ServerID() == oldLeader {
					t.Log("Starting server", oldLeader)
					go s.Start()
				}
			}

			/** we wait a bit and check that leader still remain the same */
			time.Sleep(2 * time.Second)

			/* check that leader didn't change */
			oldLeader = current_leader
			current_leader = 0
			converged = false
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				defer ticker.Stop()
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if isLeader == true {
						t.Log("new leader elected", s.LeaderId, oldLeader)
						current_leader = leaderId
						converged = true
						break
					}
				}
			}

			// otherwise passed
			if current_leader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", current_leader)
			}

			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	t.Log("Shutdown all servers.")
	// shutdown all
	//for _, s := range servers {
	//	s.Shutdown()
	//}
	t.Log("Done.")
}

func checLeader(t *testing.T, repeat int, timeout time.Duration, servers []*server.Server) (bool, uint64) {

	var converged bool = false
	var current_leader uint64 = 0
	// repeat tt.repeat times
	for i := 0; i < repeat && converged == false; i++ {
		// go sleep , wake up and check all 3 servers
		ticker := time.NewTicker(timeout)
		defer ticker.Stop()
		<-ticker.C

		for _, s := range servers {
			leaderId, _, isLeader := s.RaftState().Status()
			t.Log("node stats", leaderId, isLeader)
			if isLeader == true {
				t.Log("leader elected", leaderId)
				current_leader = leaderId
				converged = true
				break
			}
		}
	}

	// otherwise passed
	if current_leader == 0 {
		t.Errorf("leader id (type %v), expected none zero id", current_leader)
	}

	// we check that only one server is leader
	numLeaders := 0
	var convergedTerm uint64 = 0
	for _, s := range servers {
		_, term, isLeader := s.RaftState().Status()
		if isLeader == true {
			numLeaders++
		}

		if convergedTerm == 0 {
			convergedTerm = term
		} else {
			if convergedTerm != term {
				t.Errorf("expected same term in all cluster member.")
			}
		}
	}

	if numLeaders > 1 {
		t.Errorf("expected single leader in the cluster")
	}

	return true, current_leader
}

// return leader
func getLeader(t *testing.T, repeat int, timeout time.Duration, servers []*server.Server) *server.Server {

	var converged bool = false
	var current_leader uint64 = 0

	for i := 0; i < repeat && converged == false; i++ {
		ticker := time.NewTicker(timeout)
		defer ticker.Stop()
		<-ticker.C

		for _, s := range servers {
			leaderId, _, isLeader := s.RaftState().Status()
			if isLeader == true {
				current_leader = leaderId
				converged = true
				break
			}
		}
	}

	// otherwise passed
	if current_leader == 0 {
		t.Errorf("leader id (type %v), expected none zero id", current_leader)
	}

	// we check that only one server is leader
	for _, s := range servers {
		_, _, isLeader := s.RaftState().Status()
		if isLeader == true {
			return s
		}
	}

	return nil
}

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestSubmit(t *testing.T) {

	type args struct {
		serverSpec server.ServerSpec
		peers      []server.ServerSpec
		port       string
		ready      <-chan interface{}
	}
	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic leader election",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			wantErr: true,
		},
	}

	time.Sleep(2 * time.Second)
	// log.SetOutput(ioutil.Discard)
	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, "/Users/spyroot/go/src/github.com/spyroot/rocinante/config.yaml", quit)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	t.Log("Number of servers", len(servers))

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			b, _ := checLeader(t, tt.repeat, tt.timeout, servers)
			if !b {
				t.Error("failed select a leader.")
			}

			leader := getLeader(t, tt.repeat, tt.timeout, servers)
			apiEndpoint := leader.RESTEndpoint()
			simpleRequest := "http://" + apiEndpoint.RestNetworkBind + "/submit/1"
			req, err := http.NewRequest("GET", simpleRequest, nil)
			if err != nil {
				t.Fatal(err)
			}

			t.Log(req)
			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	t.Log("Shutdown all servers.")
	// shutdown all
	//for _, s := range servers {
	//	s.Shutdown()
	//}
	t.Log("Done.")
}

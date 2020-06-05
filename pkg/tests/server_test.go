/***
	Unit test for server side state transition validation.

	Each test start N number of server on local host,  it need
	valid config artifact.

    Mustafa Bayramov
*/
package tests

import (
	"flag"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"../client"
	"../server"
)

var seqMutex sync.Mutex

func seq() func() {
	seqMutex.Lock()
	return func() {
		seqMutex.Unlock()
	}
}

// default location
const DefaultConfig string = "/Users/spyroot/go/src/github.com/spyroot/rocinante/config.yaml"

// enable verbose log output.  might be very chatty
const TestVerbose bool = false

//	mainly to make glog happy
func usage() {}

/**
mainly to make glog happy
*/
func init() {
	log.Printf("test")
	flag.Usage = usage
	_ = flag.Set("test.v", "true")
}

/**
Simple leader election test.
*/
func TestLeaderElection(t *testing.T) {

	defer seq()()

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

	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, TestVerbose)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	if len(servers) == 0 {
		t.Errorf("zero server runing")
	}

	if TestVerbose {
		t.Log("Number of servers", len(servers))
	}

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var converged = false
			var currentLeader uint64 = 0
			// repeat tt.repeat times
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if TestVerbose {
						t.Log("node stats", leaderId, isLeader)
					}
					if isLeader == true {
						if TestVerbose {
							t.Log("leader elected", s.LeaderId)
						}
						currentLeader = leaderId
						converged = true
						break
					}
				}
				ticker.Stop()
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

			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	if TestVerbose {
		t.Log("Shutdown all servers.")
	}

	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}

	// we need wait a bit so all port released back to tcp stack
	time.Sleep(1 * time.Second)
}

/**
    Test start 3 server.
	- wait to election to converge
	- fail one and check re-election. We expect one of the node switch.
	- Note if run all test at same time,
      TCP stack need release all ports.
*/
func TestLeaderElection2(t *testing.T) {

	defer seq()()

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

	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, TestVerbose)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	time.Sleep(2 * time.Second)
	if TestVerbose {
		t.Log("Number of servers", len(servers))
	}

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var converged = false
			var currentLeader uint64 = 0
			// wait for all server to converge
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					t.Log("node stats", leaderId, isLeader)
					if isLeader == true {
						t.Log("leader elected", leaderId)
						currentLeader = leaderId
						converged = true
						break
					}
				}
				ticker.Stop()
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
				// check we converge on the term
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

			// re-election, shutdown a node
			for _, s := range servers {
				_, _, isLeader := s.RaftState().Status()
				// shutdown.
				if isLeader {
					s.Shutdown()
				}
			}

			// repeat, we should have two server and leader re-elected
			oldLeader := currentLeader
			currentLeader = 0
			converged = false
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
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
				ticker.Stop()
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

    Test start 3 server,
	- Fail one and check re-election,
	- Re-add server back
	- Check that cluster in normal state.

 	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestLeaderStartStop(t *testing.T) {

	defer seq()()

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
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, TestVerbose)
	if err != nil {
		t.Errorf("NewServer() error during setup %v", err)
	}

	if len(servers) == 0 {
		t.Errorf("zero server runing")
	}

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			var converged = false
			var currentLeader uint64 = 0
			// repeat tt.repeat times
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					t.Log("node stats", leaderId, isLeader)
					if isLeader == true {
						t.Log("leader elected", leaderId)
						currentLeader = leaderId
						converged = true
						break
					}
				}

				ticker.Stop()
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

			/************* re-election   **************/
			for _, s := range servers {
				_, _, isLeader := s.RaftState().Status()
				// shutdown.
				if isLeader {
					s.Shutdown()
				}
			}

			// repeat, we should have two server and leader re-elected
			oldLeader := currentLeader
			currentLeader = 0
			converged = false
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if isLeader == true {
						t.Log("new leader elected", s.LeaderId, oldLeader)
						currentLeader = leaderId
						converged = true
						break
					}
				}

				ticker.Stop()
			}

			// otherwise passed
			if currentLeader == 0 {
				t.Errorf("leader id (type %v), expected none zero id", currentLeader)
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
			oldLeader = currentLeader
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
						t.Log("new leader elected", s.LeaderId, oldLeader)
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
	//for _, s := range servers {
	//	s.Shutdown()
	//}
	t.Log("Done.")
}

/**

 */
func checkLeader(t *testing.T, repeat int, timeout time.Duration, servers []*server.Server, verbose bool) (bool, uint64) {

	var converged bool = false
	var current_leader uint64 = 0
	// repeat tt.repeat times
	for i := 0; i < repeat && converged == false; i++ {
		ticker := time.NewTicker(timeout)
		<-ticker.C
		for _, s := range servers {
			leaderId, _, isLeader := s.RaftState().Status()
			if verbose {
				t.Log("node stats", leaderId, isLeader)
			}
			if isLeader == true {
				if verbose {
					t.Log("leader elected", leaderId)
				}
				current_leader = leaderId
				converged = true
				break
			}
		}
		ticker.Stop()
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

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestSubmit(t *testing.T) {

	defer seq()()

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
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, false)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}
	if TestVerbose {
		t.Log("Number of servers", len(servers))
	}

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			b, _ := checkLeader(t, tt.repeat, tt.timeout, servers, false)
			if !b {
				t.Error("failed select a leader.")
			}

			leader := getLeader(t, tt.repeat, tt.timeout, servers, false)
			apiEndpoint := leader.RESTEndpoint()

			apiClient, err := client.NewRestClientFromUrl(apiEndpoint.RestNetworkBind)
			if err != nil {
				t.Error("failed build api client ", err)
				return
			}

			ok, err := storeAndCheck(t, apiClient, "test", "test123", 2000)
			if err != nil {
				t.Error("failed test ", err)
			}
			if ok == false {
				t.Error("failed fetch value ")
			}

			_ = CheckStorageConsistency(t, servers, false)

			////we check that all server serialized.
			//err = checkConsistency(t, servers, false)
			//if err != nil {
			//	t.Error("consistency failed", err)
			//}

			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	if TestVerbose {
		t.Log("Shutdown all servers.")
	}

	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}
	t.Log("Done.")
}

/**
Return leader after cluster converged
*/
func getLeader(t *testing.T, repeat int, timeout time.Duration, servers []*server.Server, verbose bool) *server.Server {

	var converged bool = false
	var currentLeader uint64 = 0

	for i := 0; i < repeat && converged == false; i++ {
		ticker := time.NewTicker(timeout)
		<-ticker.C
		for _, s := range servers {
			leaderId, _, isLeader := s.RaftState().Status()
			if isLeader == true {
				currentLeader = leaderId
				converged = true
				break
			}
		}
		ticker.Stop()
	}

	// otherwise passed
	if currentLeader == 0 {
		t.Errorf("leader id (type %v), expected none zero id", currentLeader)
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
Store value in cluster , fetch it back after it committed and compare
*/
func storeAndCheck(t *testing.T, apiClient *client.RestClient, key string, val string, wait time.Duration) (bool, error) {

	if apiClient == nil {
		return false, fmt.Errorf("failed client is nil")
	}

	origVal := []byte(val)
	ok, err := apiClient.Store(key, origVal)
	if err != nil {
		return false, err
	}
	if ok == false {
		return false, fmt.Errorf("failed to store value")
	}

	time.Sleep(wait * time.Millisecond)
	resp, httpErr := apiClient.Get(key)

	if httpErr != nil {
		return false, err
	}
	if resp.Success == false {
		return false, fmt.Errorf("failed featch value stored")
	}

	fromServer := string(resp.Value)
	if val == fromServer {
		return true, nil
	}

	return true, fmt.Errorf("value missmatched")
}

/**
	Check commit record in memory storage.
    Note test need wait a bit after commit, in order message populated
    in all cluster members
*/
func CheckStorageConsistency(t *testing.T, servers []*server.Server, verbose bool) error {

	var inMemoryStorage = make([]map[string][]byte, len(servers))
	var inMemorySize = make([]int, len(servers), len(servers))

	// get all in memory db
	for i, _ := range servers {
		inMemoryStorage[i] = servers[i].GetInMemoryStorage()
		inMemorySize[i] = len(inMemoryStorage[i])
		if verbose {
			t.Logf("in memory storage size %d", inMemorySize[i])
		}
	}

	return nil
}

/**

 */
func CheckConsistency(t *testing.T, servers []*server.Server, verbose bool) error {

	var copyNextIndex = make([]map[uint64]uint64, len(servers))
	var copyMatchIndex = make([]map[uint64]uint64, len(servers))
	var indexSize = make([]int, len(servers), len(servers))
	var matchSize = make([]int, len(servers), len(servers))

	// copy all index maps from all servers
	for i, s := range servers {
		copyNextIndex[i], copyMatchIndex[i] = s.RaftState().GetLogIndexCopy()
		indexSize[i] = len(copyNextIndex[i])
		matchSize[i] = len(copyMatchIndex[i])
		t.Logf("index size %d match size %d", indexSize[i], matchSize[i])
	}

	iSize := indexSize[0]
	mSize := matchSize[0]
	if verbose {
		t.Logf("index size %d match size %d", iSize, matchSize)
	}
	for i, _ := range servers {
		if iSize != indexSize[i] {
			return fmt.Errorf("index size missmatch peer [%v] %d %d", servers[i].ServerID(), iSize, indexSize[i])
		}
		if mSize != matchSize[i] {
			return fmt.Errorf("match index missmatch peer [%v] %d %d", servers[i].ServerID(), mSize, matchSize[i])
		}
	}

	return nil
}

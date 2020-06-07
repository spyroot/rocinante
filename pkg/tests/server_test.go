/***
	Unit test for server side state transition validation.

	Each test start N number of server on local host,  it need
	valid config artifact.

    Mustafa Bayramov
*/
package tests

import (
	"bytes"
	"context"
	b64 "encoding/base64"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"testing"
	"time"

	pb "../../api"
	"../client"
	"../server"
	"google.golang.org/grpc/connectivity"
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
const testVerbose bool = false

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

	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic leader election",
			timeout: 2 * time.Second, // two second
			repeat:  10,
			wantErr: true,
		},
	}

	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, testVerbose)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	if len(servers) == 0 {
		t.Errorf("zero server runing")
	}
	if testVerbose {
		t.Log("Number of servers", len(servers))
	}

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			defer seq()()

			var converged = false
			var currentLeader uint64 = 0
			// repeat tt.repeat times
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if testVerbose {
						t.Log("node stats", leaderId, isLeader)
					}
					if isLeader == true {
						if testVerbose {
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

	if testVerbose {
		t.Log("Shutdown all servers.")
	}

	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}

	// we need wait a bit so all port released back to tcp stack
	time.Sleep(2 * time.Second)
}

/**
    Test start with 3 server.
	- wait to election to converge
	- fail one and check re-election. We expect one of the node switch.
	- Note if run all test at same time,
      TCP stack need release all ports.
*/
func TestLeaderElection2(t *testing.T) {

	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic leader election2",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			wantErr: true,
		},
	}

	// channel waiting signal to shutdown
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, testVerbose)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}

	time.Sleep(2 * time.Second)
	if testVerbose {
		t.Log("Number of servers", len(servers))
	}

	// execute tests
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			defer seq()()

			var converged = false
			var currentLeader uint64 = 0

			// wait for all server to converge
			for i := 0; i < tt.repeat && converged == false; i++ {
				// go sleep , wake up and check all 3 servers
				ticker := time.NewTicker(tt.timeout)
				<-ticker.C
				for _, s := range servers {
					leaderId, _, isLeader := s.RaftState().Status()
					if testVerbose {
						t.Log("node stats", leaderId, isLeader)
					}
					if isLeader == true {
						if testVerbose {
							t.Log("leader elected", leaderId)
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
				// get current server status if leader count
				_, term, isLeader := s.RaftState().Status()
				if isLeader == true {
					numLeaders++
				}
				// check what term did cluster converged.
				if convergedTerm == 0 {
					convergedTerm = term
				} else {
					if convergedTerm != term {
						t.Errorf("expected same term in all cluster member.")
					}
				}
			}

			// check
			if numLeaders > 1 {
				t.Errorf("expected single leader in the cluster")
			}

			// re-election, shutdown a node
			for _, s := range servers {
				_, _, isLeader := s.RaftState().Status()
				// shutdown primary server
				if isLeader {
					s.Shutdown()
				}
			}

			// re check who is leader now, we should have two server and leader re-elected
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
						if testVerbose {
							t.Log("new leader elected", leaderId, oldLeader)
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

	time.Sleep(2 * time.Second)
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
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, testVerbose)
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

			/************* find a leader and shutdown  **************/
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
	t.Log("Done.")
}

/**

 */
func checkLeader(t *testing.T, repeat int, timeout time.Duration, servers []*server.Server, verbose bool) (bool, uint64) {

	var converged bool = false
	var currentLeader uint64 = 0
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

	return true, currentLeader
}

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestSubmit(t *testing.T) {

	tests := []struct {
		name    string
		timeout time.Duration // second, ms etc
		repeat  int           // how many time repeat
		wantErr bool
	}{
		{ // test
			name:    "basic submit test",
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
	if testVerbose {
		t.Log("Number of servers", len(servers))
	}

	// execute tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer seq()()

			b, _ := checkLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			if !b {
				t.Error("failed select a leader.")
			}

			leader := getLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			apiEndpoint := leader.RESTEndpoint()

			apiClient, err := client.NewRestClientFromUrl(apiEndpoint.RestNetworkBind)
			if err != nil {
				t.Error("failed build api client ", err)
				return
			}

			ok, err := storeAndCheck(t, apiClient, "test", "test123", 2000, testVerbose)
			if err != nil {
				t.Error("failed test ", err)
			}
			if ok == false {
				t.Error("failed fetch value ")
			}

			err = CheckStorageConsistency(t, servers, false)
			if err != nil {
				t.Error("failed consistency check", err)
			}

			//we check that all server serialized.
			err = CheckConsistency(t, servers, false)
			if err != nil {
				t.Error("consistency failed", err)
			}

			// end
		})

		teardownTestCase(t)
		// when all test are finished teardown closes a channels.
	}
	// wait all test to finish
	<-quit

	if testVerbose {
		t.Log("Shutdown all servers.")
	}

	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}
	t.Log("Done.")
}

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestSubmit2(t *testing.T) {

	tests := []struct {
		name     string
		timeout  time.Duration // second, ms etc
		repeat   int           // how many time repeat
		records  int
		wantErr  bool
		batch    bool          // batch load
		wait     time.Duration // wait between request
		deadline time.Duration
	}{
		{ // test
			name:    "sequential submit 1000 ms wait 10 record",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			records: 10,
			wantErr: false,
			batch:   false,
			wait:    1000,
		},
		{ // test
			name:    "sequential submit 500 ms wait 10 record",
			timeout: 1 * time.Second, // two second
			repeat:  10,
			records: 10,
			wantErr: false,
			batch:   false,
			wait:    500,
		},
		{ // test
			name:    "sequential 1000 records 500 ms wait 1000 record",
			timeout: 1 * time.Second, // time to wait cluster to converge
			repeat:  10,
			records: 1000,
			wantErr: false,
			batch:   false,
			wait:    500,
		},
		{ // test
			name:     "sequential 10000 batch store 2 ms wait 10000 record",
			timeout:  1 * time.Second, // time to wait cluster to converge
			repeat:   10,
			records:  100,
			wantErr:  false,
			batch:    true, // batch store , than check consistency
			wait:     2,    // we ca use less RTT since no round trip need to fetch
			deadline: 10,
		},
	}

	time.Sleep(2 * time.Second)
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, false)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}
	if testVerbose {
		t.Log("Number of servers", len(servers))
	}

	defer seq()()

	// execute tests
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			rand.Seed(time.Now().UnixNano())

			b, _ := checkLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			if !b {
				t.Error("failed select a leader.")
			}

			leader := getLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			apiEndpoint := leader.RESTEndpoint()

			apiClient, err := client.NewRestClientFromUrl(apiEndpoint.RestNetworkBind)
			if err != nil {
				t.Error("failed build api client ", err)
				return
			}

			generatedKVs := make([]string, 0)
			start := time.Now()
			for i := 0; i < tt.records; i++ {
				key := randSeq(12)
				val := randSeq(12)
				generatedKVs = append(generatedKVs, key)

				if !tt.batch {
					ok, err := storeAndCheck(t, apiClient, key, val, tt.wait, testVerbose)
					if err != nil {
						t.Error("failed test ", err)
					}
					if ok == false {
						t.Error("failed fetch value ")
					}
				} else {
					ok, err := batchStore(t, apiClient, key, val, tt.wait, testVerbose)
					if err != nil {
						t.Error("failed test ", err)
					}
					if ok == false {
						t.Error("failed fetch value ")
					}
				}
			}

			for i, k := range generatedKVs {
				encodedKey := b64.StdEncoding.EncodeToString([]byte(k))
				generatedKVs[i] = encodedKey
			}

			elapsed := time.Since(start)
			t.Logf("Took %s for %d average time", elapsed, tt.records)

			var converged bool

			if tt.batch {
				start := time.Now()
				for i := 0; i < tt.repeat; i++ {
					converged = true
					for _, k := range generatedKVs {
						err := CheckKeyValConsistency(t, servers, k, tt.deadline, false)
						if err != nil {
							converged = false
							break
						}
					}

					if converged == true {
						t.Logf("Took %s for %d average time", elapsed, tt.records)
						break
					}

					elapsed := time.Since(start)
					t.Logf("Took %s for records %d still not converged", elapsed, tt.records)
					//
					time.Sleep(10 * time.Second)
				}
			}
			if converged == false {
				t.Error("Cluster never converged", err)
			}
			// check store
			err = CheckStorageConsistency(t, servers, false)
			if err != nil {
				t.Error("failed consistency check", err)
			}
			//we check that all server serialized.
			err = CheckConsistency(t, servers, false)
			if err != nil {
				t.Error("consistency failed", err)
			}
			// end
		})

		// when all test are finished teardown and close a channels.
		if i == len(tests)-1 {
			t.Log("donn")
			teardownTestCase(t)
		}
	}

	// wait all test to finish
	<-quit

	if testVerbose {
		t.Log("Shutdown all servers.")
	}

	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}

	t.Log("Done.")
}

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestSimpleAsyncGet(t *testing.T) {

	tests := []struct {
		name     string
		timeout  time.Duration // second, ms etc
		repeat   int           // how many time repeat
		records  int
		wantErr  bool
		batch    bool          // batch load
		wait     time.Duration // wait between request
		deadline time.Duration
	}{
		{ // test
			name:     "64 record 1 ms dead line",
			timeout:  1 * time.Second, // time to wait cluster to converge
			repeat:   10,              // converged for get value
			records:  64,              // number of record
			wantErr:  false,
			batch:    true, // batch store , than check consistency
			wait:     2,    // wait after store
			deadline: 1,    // 1 ms
		},
		{ // test
			name:     "1000 record 1 ms dead line ",
			timeout:  1 * time.Second, // time to wait cluster to converge
			repeat:   10,              // converged for get value
			records:  1000,            // number of record
			wantErr:  false,
			batch:    true, // batch store , than check consistency
			wait:     2,    // wait after store
			deadline: 1,    // 1 ms
		},
		{ // test
			name:     "10000 record 1 ms dead line ",
			timeout:  1 * time.Second, // time to wait cluster to converge
			repeat:   10,              // converged for get value
			records:  10000,           // number of record
			wantErr:  false,
			batch:    true, // batch store , than check consistency
			wait:     10,   // wait after store
			deadline: 2,    // 1 ms
		},
		{ // test
			name:     "100000 record 1 ms dead line ",
			timeout:  1 * time.Second, // time to wait cluster to converge
			repeat:   10,              // converged for get value
			records:  100000,          // number of record
			wantErr:  false,
			batch:    true, // batch store , than check consistency
			wait:     10,   // wait after store
			deadline: 2,    // 1 ms
		},
	}

	time.Sleep(2 * time.Second)
	quit := make(chan interface{})
	teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, false)
	if err != nil {
		t.Fatal("NewServer() error during setup", err)
	}
	if testVerbose {
		t.Log("Number of servers", len(servers))
	}

	defer seq()()

	// execute tests
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			rand.Seed(time.Now().UnixNano())

			b, _ := checkLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			if !b {
				t.Error("failed select a leader.")
			}

			leader := getLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			apiEndpoint := leader.RESTEndpoint()

			apiClient, err := client.NewRestClientFromUrl(apiEndpoint.RestNetworkBind)
			if err != nil {
				t.Error("failed build api client", err)
			}

			generatedKVs := make([]string, 0)
			for i := 0; i < tt.records; i++ {
				key := randSeq(12)
				val := randSeq(12)
				generatedKVs = append(generatedKVs, key)
				ok, err := batchStore(t, apiClient, key, val, tt.wait, testVerbose)
				if err != nil {
					t.Error("failed test", err)
				}
				if ok == false {
					t.Error("failed store key-value")
				}
			}

			// encode all the keys
			for i, k := range generatedKVs {
				encodedKey := b64.StdEncoding.EncodeToString([]byte(k))
				generatedKVs[i] = encodedKey
			}

			// wait a bit tt.wait time
			time.Sleep(tt.wait * time.Second)

			start := time.Now()
			var converged = false

			binaryConverged := tt.wait
			for i := 0; i < tt.repeat; i++ {
				converged = true
				for _, k := range generatedKVs {
					err := CheckKeyValConsistency(t, servers, k, tt.deadline, false)
					if err != nil {
						converged = false
						break
					}
				}

				if converged == true {
					elapsed := time.Since(start)
					t.Logf("Took %s for %d record. average time per lookup %d microsed",
						elapsed, tt.records, elapsed.Microseconds()/int64(tt.records))
					break
				}

				t.Logf("Took %s for records %d still.. not converged", time.Since(start), tt.records)
				binaryConverged = binaryConverged * 2
				time.Sleep(binaryConverged * time.Second)
			}
			// end
		})

		// when all test are finished teardown and close a channels.
		if i == len(tests)-1 {
			t.Log("done")
			teardownTestCase(t)
		}
	}

	// wait all test to finish
	<-quit

	if testVerbose {
		t.Log("Shutdown all servers.")
	}

	// shutdown all
	for _, s := range servers {
		s.Shutdown()
	}

	t.Log("Done.")
}

/**
    Test start 3 server,  fail one and check re-election , re-add back
	Note if run all test at same time,  TCP stack need release all ports.
*/
func TestConnectReconnect(t *testing.T) {

	tests := []struct {
		name       string
		timeout    time.Duration // second, ms etc
		repeat     int           // how many time repeat
		records    int
		waitLeader time.Duration // wait for leader initial timer
		reconnect  bool          // reconnect back a node we disconnected
	}{
		{ // test
			name:       "DisconnectConnectBack1",
			timeout:    1 * time.Second, // time to wait cluster to converge
			repeat:     10,              // converged for get value
			waitLeader: 2,               // wait for leader initial timer
			reconnect:  false,
		},
		{ // test
			name:       "DisconnectConnectBack2",
			timeout:    1 * time.Second, // time to wait cluster to converge
			repeat:     10,              // converged for get value
			waitLeader: 2,               // wait for leader initial timer
			reconnect:  true,
		},
	}

	var quit chan interface{}
	var wait sync.WaitGroup

	// execute tests
	for _, tt := range tests {
		wait.Add(1)
		quit = make(chan interface{})

		t.Run(tt.name, func(t *testing.T) {
			defer wait.Done()
			teardownTestCase, servers, err := server.SetupTestCase(t, DefaultConfig, quit, true)
			if err != nil {
				t.Errorf("NewServer() error during setup %v", err)
				return
			}
			if len(servers) < 1 {
				t.Errorf("number of server is less than one, check for port conflict.")
				return
			}
			if testVerbose {
				t.Log("Number of servers running", len(servers))
			}
			defer teardownTestCase(t)

			rand.Seed(time.Now().UnixNano())
			time.Sleep(tt.waitLeader * time.Second)
			b, _ := checkLeader(t, tt.repeat, tt.timeout, servers, testVerbose)
			if !b {
				t.Error("failed select a leader.")
				return
			}

			// time.Sleep(10 * )
			var s *server.Server
			s, err = DisconnectLeader(t, servers, true)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			if s == nil {
				t.Errorf("server is nil")
				return
			}
			if tt.reconnect {
				// reconnect back
				s.Reconnect()
				time.Sleep(5 * time.Second)
				if err = IsConnected(t, s, servers, testVerbose); err != nil {
					t.Errorf(err.Error())
				}
			}

			// wait a bit and check
			time.Sleep(2 * time.Second)
			leaders := 0
			for _, s := range servers {
				_, _, ok := s.IsLeader()
				if ok {
					leaders++
				}
			}

			if tt.reconnect {
				// if cluster partition we should have two leader
				if leaders > 1 {
					t.Errorf("expected 1 leader got %d", leaders)
					return
				}
			} else {
				// if node reconnected we should have one leader
				if leaders != 2 {
					t.Errorf("expected two leader got %d", leaders)
					return
				}
			}

			if testVerbose {
				t.Log("Shutdown all servers.")
			}

			for _, s := range servers {
				s.Shutdown()
			}

			// wait for all port to be released
			for released := 0; released <= len(servers); {
				for _, s := range servers {
					if server.CheckSocket(s.ServerBind(), "tcp") {
						t.Log("server released port ", s.ServerBind())
						released++
					}
				}
			}

			for released := 0; released <= len(servers); {
				for _, s := range servers {
					if server.CheckSocket(s.RESTEndpoint().RestNetworkBind, "tcp") {
						t.Log("server released port ", s.ServerBind())
						released++
					}
				}
			}
			servers = nil
		})

		<-quit
		// when all test are finished teardown and close a channels.
		// wait all test to finish
	}

	t.Log("Done.")
}

/**
Check if server connected to a cluster
*/
func IsConnected(t *testing.T, server *server.Server, servers []*server.Server, verbose bool) error {

	// find a leader and disconnect
	leaderPresent := false
	for _, s := range servers {
		if _, _, ok := s.IsLeader(); ok {
			leaderPresent = true
		}
	}

	if !leaderPresent {
		return fmt.Errorf("no leader in the cluster")
	}

	// check old leader disconnected from the rest
	if server != nil {
		// wait
		var ready = 0
		var sleep time.Duration = 150
		for i := 0; i < 10; i++ {
			peers := server.PeerStatus()
			for i, _ := range peers {
				if peers[i].State == connectivity.Ready {
					ready++
				}
			}
			// number of ready must same as number of node in cluster
			if ready == len(servers) {
				break
			}
			// sleep and increase counter , so we wait convergence
			time.Sleep(sleep * time.Millisecond)
		}

		if ready != len(servers) {
			return fmt.Errorf("failed disconnect. number of ready node %v", ready)
		}
	}

	return nil
}

/**
	Disconnect leader and if it confirmed return pointer to a server and no error
    otherwise will return error.
*/
func DisconnectLeader(t *testing.T, servers []*server.Server, verbose bool) (*server.Server, error) {

	if len(servers) <= 1 {
		t.Log("Number of server suppose to be more than 1.")
	}

	// find a leader and disconnect
	var oldLeader *server.Server = nil
	for i, s := range servers {
		if _, _, ok := s.IsLeader(); ok {
			s.Disconnect()
			oldLeader = servers[i]
			break
		}
		if verbose {
			t.Log("found leader")
		}
	}

	// check old leader disconnected from the rest
	time.Sleep(1 * time.Second)
	if oldLeader != nil {
		if oldLeader.IsDisconnected() == false {
			return nil, fmt.Errorf("server must be already in disconnect state")
		} else {
			if verbose {
				t.Log("server disconnected")
			}
		}

		// wait
		var ready = 0
		var sleep time.Duration = 100
		for i := 0; i < 10; i++ {
			ready = 0
			peers := oldLeader.PeerStatus()
			for i, _ := range peers {
				if peers[i].State == connectivity.Ready {
					ready++
				}
			}
			if ready <= 1 {
				if verbose {
					t.Log("all connection closed")
				}
				break
			}
			// sleep and increase counter , so we wait convergence
			time.Sleep(sleep * time.Millisecond)
		}

		if ready > 1 {
			return nil, fmt.Errorf("failed disconnect. number of ready node %v", ready)
		}
	}

	return oldLeader, nil
}

/**
Should return a cluster leader, after all node converge to stable state
*/
func getLeader(t *testing.T, repeat int, timeout time.Duration, servers []*server.Server, verbose bool) *server.Server {

	var converged = false
	var currentLeader uint64 = 0

	for i := 0; i < repeat && converged == false; i++ {
		ticker := time.NewTicker(timeout)
		<-ticker.C
		for _, s := range servers {
			leaderId, term, isLeader := s.RaftState().Status()
			if isLeader == true {
				if verbose {
					t.Logf("found leader, leader id %v, term %v", leaderId, term)
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

	if verbose {
		t.Logf("Checking single leader invariant")
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
func storeAndCheck(t *testing.T, apiClient *client.RestClient,
	key string, val string, wait time.Duration, verbose bool) (bool, error) {

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

	if verbose {
		t.Log("Key stored , going to fetch it back from cluster")
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
Store value in cluster , fetch it back after
it committed and compare.

wait, wait a bit
*/
func batchStore(t *testing.T, apiClient *client.RestClient,
	key string, val string, wait time.Duration, verbose bool) (bool, error) {

	if apiClient == nil {
		return false, fmt.Errorf("failed client is nil")
	}

	origVal := []byte(val)
	ok, err := apiClient.Store(key, origVal)
	if err != nil {
		return false, err
	}
	if ok == false {
		if verbose {
			t.Log("no error but failed to store.")
		}
		return false, fmt.Errorf("failed to store value")
	}

	time.Sleep(wait * time.Millisecond)
	return true, nil
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
	for i := range servers {
		inMemoryStorage[i] = servers[i].GetInMemoryStorage()
		inMemorySize[i] = len(inMemoryStorage[i])
		if verbose {
			t.Logf("in memory storage size %d", inMemorySize[i])
		}
	}

	return nil
}

type void struct{}

/**

 */
func MissingValue(a, b []pb.LogEntry) ([]string, error) {

	mapA := make(map[string]void, len(a))
	var diffs []string

	for i := range a {
		mapA[a[i].Command.Key] = void{}
	}

	// find MissingValue values in a
	for i := range b {
		if _, ok := mapA[b[i].Command.Key]; !ok {
			diffs = append(diffs, b[i].Command.Key)
		}
	}

	if len(diffs) != 0 {
		return diffs, nil
	}

	for i := range a {
		diff := bytes.Compare(b[i].Command.Value, a[i].Command.Value)
		if diff != 0 {
			return diffs, fmt.Errorf("value missmatch")
		}
	}

	return diffs, nil
}

/**
Iterate over all server and checks given key and value
in committed storage and log, if message consistent report true
no error otherwise report error
*/
func CheckKeyValConsistency(t *testing.T, servers []*server.Server, key string, d time.Duration, verbose bool) error {

	// copy all index maps from all servers
	responds := make([]*server.GetValueRespond, len(servers))

	var err error
	for i, s := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), d*time.Millisecond)
		if verbose {
			t.Log("Checking key", key)
		}
		responds[i], err = s.GetValue(ctx, key)
		if err != nil {
			if verbose {
				t.Logf("got error for key %v %v", key, err)
			}
			return err
		}
		if verbose {
			t.Logf("Got respond back %d index in log %d", responds[i].Val, responds[i].Index)
		}
		cancel()
	}

	// we take from first respond and compare all should match,
	// doesn't matter from whom we use for a reference value
	val := responds[0].Val
	index := responds[0].Index
	success := responds[0].Success

	for i := 1; i < len(responds); i++ {
		diff := bytes.Compare(responds[i].Val, val)
		if diff != 0 {
			t.Logf("mistmatch between servers %v %v, expected %v got %v",
				servers[0], servers[i], val, responds[i].Val)
			return fmt.Errorf("mistmatch between servers %v %v, expected %v got %v",
				servers[0], servers[i], val, responds[i].Val)
		}
		if responds[i].Index != index {
			t.Logf("mistmatch between servers %v %v, expected %v got %v",
				servers[0], servers[i], index, responds[i].Index)
			return fmt.Errorf("mistmatch between servers %v %v, expected %v got %v",
				servers[0], servers[i], index, responds[i].Index)
		}
		if responds[i].Success != success {
			t.Logf("mistmatch between servers %v %v, expected %v got %v",
				servers[0], servers[i], success, responds[i].Success)
			return fmt.Errorf("mistmatch between servers %v %v, expected %v got %v",
				servers[0], servers[i], success, responds[i].Success)
		}
	}

	return nil
}

/**
Check log consistency in all servers
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
		//t.Logf("index size %d match size %d", indexSize[i], matchSize[i])
	}

	logEntry := make([][]pb.LogEntry, len(servers))
	storageSize := -1

	for i, s := range servers {
		logEntry[i] = s.RaftState().GetLogCopy()
		if storageSize == -1 {
			storageSize = len(logEntry[i])
		} else {
			if storageSize != len(logEntry[i]) {
				return fmt.Errorf("storages with a different sizez")
			}
		}

		indexSize[i] = len(copyNextIndex[i])
		matchSize[i] = len(copyMatchIndex[i])
		if verbose {
			t.Logf("commit storage size %d", len(logEntry[i]))
		}
	}

	// compare log on all server list
	for i := 0; i < len(servers)-1; i++ {
		a := logEntry[i]
		b := logEntry[i+1]
		if verbose {
			t.Log("checking servers log", servers[i].ServerID(), servers[i+1].ServerID())
		}

		diff, err := MissingValue(a, b)
		if err != nil {
			return err
		}
		if len(diff) > 0 {
			return fmt.Errorf("storage has different keys")
		}
	}

	return nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

/**
  Generate random string
*/
func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

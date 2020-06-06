/*
	Raft RaftProtocol implementation
	The implementation follow original paper

	Mustafa Bayramov
*/
package server

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	pb "../../api"
	"../../pkg/color"
	"github.com/golang/glog"
)

type ProtocolState int
type ProtocolMessage int

const (
	Follower ProtocolState = iota
	Candidate
	Leader
	Dead
)

// factor used if we want slow down election,  default is 1
const FACTOR = 1
const REFESH_TIME = 10 * FACTOR
const HEARBEAT_INTERVAL = 50 * FACTOR

const ElectionLog bool = false

const (
	RequestVoteReq ProtocolMessage = iota
	RequestVoteRepl
	AppendEntriesReq
	AppendEntriesRepl
)

type RaftProtocol struct {

	// mutex protects concurrent access to a CM.
	mu sync.Mutex

	// pointer to this server
	server *Server

	// id is the server ID of this CM.
	id uint64

	// raft peers
	peers map[string]uint64

	// server is the server containing this CM.
	// It's used to issue RPC calls to peers.
	//server *Server

	// Persistent Raft state on all servers
	currentTerm uint64
	votedFor    uint64
	stateLog    []*pb.LogEntry

	// Volatile Raft state on all servers
	state ProtocolState
	// last election reset
	electionResetEvent time.Time
	// for each server, index of next log entry
	nextIndex map[uint64]uint64
	// for each server, index of highest log entry
	matchIndex map[uint64]uint64

	// introduce variable jitter for unit testing
	Jitter bool
	// make this server force election process
	Force bool // force elect
	// simulate random packet drop
	Drop bool

	// channel where we signal about commit
	// we use same concept for 2pc protocol
	// channel a used for all prepare commit msg, we collect majority ready
	// we than send commit msg on another channel , when we collect majority
	// we write to commit ready
	commitReadyChan chan CommitReady
	commitChan      chan<- CommitEntry

	//commitChan chan CommitEntry

	// number of peers
	numberPeers int

	// commit and append index
	volatileState RaftVolatileState
}

/**
Returns copy fof match and index maps.
it mainly for debug to compare state between
different servers.
*/
func (raft *RaftProtocol) GetLogIndexCopy() (map[uint64]uint64, map[uint64]uint64) {
	raft.mu.Lock()
	defer raft.mu.Unlock()

	var copyNextIndex = make(map[uint64]uint64)
	var copyMatchIndex = make(map[uint64]uint64)

	for v, k := range raft.nextIndex {
		copyNextIndex[k] = v
	}

	for v, k := range raft.matchIndex {
		copyMatchIndex[k] = v
	}

	return copyNextIndex, copyMatchIndex
}

/*
	Returns copy of entire log.  It mainly useful for unit testing
    for consistency check
*/
func (raft *RaftProtocol) GetLogCopy() []pb.LogEntry {

	raft.mu.Lock()
	defer raft.mu.Unlock()
	var stateCopy []pb.LogEntry
	for _, e := range raft.stateLog {
		stateCopy = append(stateCopy, *e)
	}
	return stateCopy
}

/*
	Return copy of volatile state.
*/
func (raft *RaftProtocol) GetVolatileState() RaftVolatileState {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	volatileState := RaftVolatileState{
		commitIndex: raft.volatileState.CommitIndex(),
		lastApplied: raft.volatileState.LastApplied(),
	}

	return volatileState
}

/**
Request vote handler, listen for request vote msg.
*/
func (raft *RaftProtocol) RequestVote(vote *pb.RequestVote) (RequestVoteReply, error) {

	var reply RequestVoteReply
	if raft == nil {
		reply.VoteGranted = true
		return reply, nil
	}

	if raft.isDead() {
		return reply, fmt.Errorf("servever state is shutdown")
	}

	raft.mu.Lock()
	myLastTerm := raft.currentTerm
	raft.log("vote request %+v [currentTerm=%d, votedFor=%d]", vote, raft.currentTerm, raft.votedFor)
	raft.mu.Unlock()

	// if term outdated make myself follower
	if vote.Term > myLastTerm || (vote.Term != math.MaxUint64 && myLastTerm == math.MaxUint64) {
		raft.log("... term out of date")
		raft.makeFollower(vote.Term)
	}

	// Receiver implementation:
	// 1. Reply false if term < currentTerm (§5.1)
	// 2. If votedFor is null or candidateId, and candidate’s log is at
	// least as up-to-date as receiver’s log, grant vote (§5.2, §5.4)

	lastLogIndex, lastLogTerm := raft.lastLogIndexAndTerm()

	raft.mu.Lock()
	defer raft.mu.Unlock()
	if raft.currentTerm == vote.Term &&
		// if my term was old I set votedFor MaxUint64 so I should
		// grant a vote for a candidate
		(raft.votedFor == math.MaxUint64 || raft.votedFor == vote.CandidateId) &&
		(vote.LastLogTerm > lastLogTerm || (vote.LastLogTerm == lastLogTerm && vote.LastLogIndex >= lastLogIndex)) {
		reply.VoteGranted = true
		raft.votedFor = vote.CandidateId
		raft.electionResetEvent = time.Now()
	} else {
		if raft.state == Follower {
			glog.Infof("Strange case %d", raft.votedFor)
		}
		reply.VoteGranted = false
	}

	reply.Term = raft.currentTerm
	raft.log("... Request vote reply: %+v...", reply)

	return reply, nil
}

/**
Handler for gRPC append entry
*/
func (raft *RaftProtocol) AppendEntries(req *pb.AppendEntries) (*pb.AppendEntriesReply, error) {

	var reply pb.AppendEntriesReply
	if raft == nil {
		return nil, fmt.Errorf("cm uninitlized")
	}

	if raft.isDead() {
		return &reply, fmt.Errorf("cm state is dead")
	}

	// If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower
	oldTerm, oldState, _ := raft.getState()
	if req.Term > oldTerm || (req.Term != math.MaxUint64 && oldTerm == math.MaxUint64) {
		raft.log("my term is out of dated, my term %v, leader term %v", oldTerm, req.Term)
		raft.makeFollower(req.Term)
	}

	// we read it again
	oldTerm, oldState, _ = raft.getState()
	reply.Success = false
	if req.Term == oldTerm {
		raft.log("... term is up to date.")
		if oldState != Follower {
			raft.makeFollower(req.Term)
		}

		// update timer
		raft.mu.Lock()
		defer raft.mu.Unlock()
		raft.electionResetEvent = time.Now()

		// predicate
		// if req.PrevLogIndex == math.MaxUint64  server just started.
		// Initial value is MaxUint64
		//   or
		// message prev log index is less than current size of current server log
		// and term matched
		if req.PrevLogIndex == math.MaxUint64 ||
			(req.PrevLogIndex < uint64(len(raft.stateLog)) && req.PrevLogTerm == raft.stateLog[req.PrevLogIndex].Term) {

			reply.Success = true
			lowerBound := req.PrevLogIndex + 1
			upperBound := 0

			for {
				if lowerBound >= uint64(len(raft.stateLog)) || upperBound >= len(req.Entries) {
					break
				}
				// compare term
				if raft.stateLog[lowerBound].Term != req.Entries[upperBound].Term {
					break
				}
				lowerBound++
				upperBound++
			}

			if upperBound < len(req.Entries) {
				raft.log("... inserting entries %v from index %d", req.Entries[upperBound:], lowerBound)
				raft.stateLog = append(raft.stateLog[:lowerBound], req.Entries[upperBound:]...)
				raft.log("... log is now: %v", raft.log)
			}

			// Send data commit index to a buffer
			// first case if leader commit index is > than this server
			// second case if we just connected our our index is MaxUint64
			// since we are not using original -1 from raft we need detect
			// this case
			if req.LeaderCommit > raft.volatileState.CommitIndex() ||
				(req.LeaderCommit != math.MaxUint64 && raft.volatileState.CommitIndex() == math.MaxUint64) {
				raft.volatileState.setCommitIndex(uint64Min(req.LeaderCommit, uint64(len(raft.stateLog)-1)))
				raft.log("... setting commitIndex=%d", raft.volatileState.CommitIndex())
				raft.commitReadyChan <- CommitReady{
					Term: oldTerm,
				}
			}
		} else {

			//if req.PrevLogIndex >= len(raft.stateLog) {
			//	reply.ConflictIndex = len(raft.stateLog)
			//	reply.ConflictTerm = -1
			//} else {
			//	// PrevLogIndex points within our log, but PrevLogTerm doesn't match
			//	// cm.log[PrevLogIndex].
			//	reply.ConflictTerm = cm.log[args.PrevLogIndex].Term
			//
			//	var i int
			//	for i = args.PrevLogIndex - 1; i >= 0; i-- {
			//		if cm.log[i].Term != reply.ConflictTerm {
			//			break
			//		}
			//	}
			//	reply.ConflictIndex = i + 1
			//}
		}
	}

	// update term
	reply.Term = oldTerm
	raft.log("------> append replay term=%d status=%v", reply.Term, reply.Success)
	return &reply, nil
}

// return true if cm in a dead state.
func (raft *RaftProtocol) isDead() bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	if raft.state == Dead {
		return true
	}
	return false
}

/**

 */
func uint64Min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

type RequestVoteReply struct {
	Term        uint64
	VoteGranted bool
}

func (s ProtocolState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	case Dead:
		return "Dead"
	default:
		panic("unreachable")
	}
}

/**
Returns new RAFT protocol module.
*/
func NewRaftProtocol(id uint64,
	peers map[string]uint64,
	server *Server, numPeer int,
	ready <-chan interface{},
	commitChan chan<- CommitEntry) (*RaftProtocol, error) {

	raft := new(RaftProtocol)
	raft.numberPeers = numPeer

	if len(peers) == 0 {
		return nil, fmt.Errorf("number of peers zero")
	}

	//
	raft.id = id
	raft.peers = peers
	raft.server = server
	raft.state = Follower
	raft.votedFor = math.MaxUint64

	raft.volatileState.setCommitIndex(math.MaxUint64)
	raft.volatileState.setLastApplied(math.MaxUint64)

	raft.nextIndex = make(map[uint64]uint64)
	raft.matchIndex = make(map[uint64]uint64)

	// ready channel
	raft.commitReadyChan = make(chan CommitReady, 32)
	raft.commitChan = commitChan

	raft.volatileState.commitIndex = math.MaxUint64
	raft.volatileState.lastApplied = math.MaxUint64

	raft.Jitter = true
	raft.Drop = false
	raft.Force = false

	// wait for signal
	go func() {
		<-ready
		// start timer and run election
		raft.electionResetEvent = time.Now()
		raft.log("Received server ready signal.")
		raft.runElectionTimer()
	}()

	// serialize channel
	raft.commitChannel()

	return raft, nil
}

// log
func (raft *RaftProtocol) log(format string, args ...interface{}) {
	format = fmt.Sprintf("[%d] ", raft.id) + format
	glog.Infof(format, args...)
}

/**

 */
func (raft *RaftProtocol) electionTimeout() time.Duration {
	if raft.Drop == false {
		// introduce original jitter
		if raft.Jitter == true {
			return time.Millisecond * time.Duration((HEARBEAT_INTERVAL*3)+rand.Intn(HEARBEAT_INTERVAL*3))
		} else {
			// introduce fix message rate
			return time.Millisecond * time.Duration(HEARBEAT_INTERVAL*3)
		}
	} else {
		// introduce delay
		time.Sleep(60 * time.Second)
		return time.Millisecond * time.Duration(HEARBEAT_INTERVAL*3)
	}
}

/*
	Check if current state expired the predicate used
    to check if we consensus need to choose new leader
*/
func (raft *RaftProtocol) isExpired(termStarted uint64) bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()

	// state changed
	if raft.state != Candidate && raft.state != Follower {
		if ElectionLog {
			raft.log("in election state change state=%s, withdraw", raft.state)
		}
		return false
	}

	// term change
	if termStarted != raft.currentTerm {
		if ElectionLog {
			raft.log("in election term changed from %d to %d, withdraw", termStarted, raft.currentTerm)
		}
		return false
	}

	return true
}

//  get current term
func (raft *RaftProtocol) getTerm() uint64 {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	return raft.currentTerm
}

// capture cm state for a given term
func (raft *RaftProtocol) getState() (uint64, ProtocolState, time.Time) {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	return raft.currentTerm, raft.state, raft.electionResetEvent
}

/**
Return true if election timer expired.
*/
func (raft *RaftProtocol) isElectionReset(duration time.Duration) bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	if elapsed := time.Since(raft.electionResetEvent); elapsed >= duration {
		delta := elapsed - duration
		raft.log("election expired -> my state %s delta %v", raft.state, delta.Seconds())
		return true
	}
	return false
}

/**
  Run election
*/
func (raft *RaftProtocol) runElectionTimer() {

	timeoutDuration := raft.electionTimeout()
	termStarted, oldState, _ := raft.getState()

	raft.log("election started (%v), term=%d", timeoutDuration, termStarted)

	ticker := time.NewTicker(REFESH_TIME * time.Millisecond)
	defer ticker.Stop()
	for {
		<-ticker.C

		raft.mu.Lock()
		currentState := raft.state
		if ElectionLog {
			raft.log("time to elect, kicked in (%v), term=%d my role=%v", timeoutDuration, termStarted, currentState)
		}

		// if we wake up and state changed, with raw from election
		if oldState != currentState {
			raft.log("withdrawing %v current state.", currentState)
			raft.mu.Unlock()
			return
		}
		raft.mu.Unlock()

		// wake up, check state.
		if !raft.isExpired(termStarted) {
			raft.log("withdrawing term expired.")
			break
		}

		// Start an election, if we haven't heard from a leader or haven't voted for
		// someone for the duration of the timeout,  start election.
		if raft.isElectionReset(timeoutDuration) {
			raft.startElection()
			break
		}
	}
}

// return if node candidate or not
func (raft *RaftProtocol) isCandidate() bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	if raft.state != Candidate {
		return false
	}
	return true
}

/*
 	Atomically update a vote and send back to a caller.
	Vote counted only for a current term, if we count a term while
    term update , server need bounce from election to next election
	cycle.
*/
func (raft *RaftProtocol) gotVote(currentTerm uint64, votes uint64, reply *RequestVoteReply) uint64 {

	raft.mu.Lock()
	defer raft.mu.Unlock()
	// check if a vote is valid
	if reply.Term == currentTerm && reply.VoteGranted {
		updateVotes := atomic.AddUint64(&votes, 1)
		return updateVotes
	}
	//
	return votes
}

/**
Atomically update next term
*/
func (raft *RaftProtocol) newTerm() {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	raft.currentTerm += 1
}

/**
Method returns last last long index and term in the log
Each log entry has a term based on spec.
*/
func (raft *RaftProtocol) lastLogIndexAndTerm() (uint64, uint64) {

	raft.mu.Lock()
	defer raft.mu.Unlock()

	if len(raft.stateLog) == 0 {
		return math.MaxUint64, math.MaxUint64
	}

	lastIndex := len(raft.stateLog) - 1
	return uint64(lastIndex), raft.stateLog[lastIndex].Term
}

/**

 */
func (raft *RaftProtocol) startElection() {

	electionTerm := raft.readCurrentTerm()
	// 1 set myself candidate
	// 2 vote for myself
	// 3 set reset event time
	// 4 set vote received to 1
	raft.mu.Lock()
	raft.state = Candidate
	raft.currentTerm += 1
	raft.electionResetEvent = time.Now()
	raft.votedFor = raft.id
	raft.log("becomes a candidate for term=%d log=%v", electionTerm, raft.log)
	var votesReceived uint64 = 1
	raft.mu.Unlock()

	// Send RequestVote RPCs to all other servers concurrently.
	// we try to avoid any lock during that cycle
	// so we can process replay as fast as we can.

	raft.log("Attempting start election")
	for _, peerId := range raft.peers {

		go func(peer uint64) {
			savedLastLogIndex, savedLastLogTerm := raft.lastLogIndexAndTerm()
			args := &pb.RequestVote{
				Term:         electionTerm,
				CandidateId:  raft.id,
				LastLogIndex: savedLastLogIndex,
				LastLogTerm:  savedLastLogTerm,
			}

			raft.log("sending vote to peer %d, term %+v, voting for %+v", peer, args.Term, args.CandidateId)
			if rep, err := raft.server.RemoteCall(peer, args); err == nil {
				voteReplay, ok := rep.(*RequestVoteReply)
				if !ok {
					raft.log("received invalid replay from peer %+v , %+T", peer, voteReplay)
					return
				}
				// re-check that we are still a candidate node if not we already lost
				raft.log("received vote reply from peer %v term %d %+v ",
					peer, voteReplay.Term, voteReplay.VoteGranted)
				if raft.isCandidate() == false {
					raft.log("node was a candidate , state already change. ")
					return
				}

				// tie we make our self follower
				// timer will trigger re-election and introduce jitter, someone will win
				if voteReplay.Term == math.MaxUint64 && electionTerm == math.MaxUint64 {
					raft.makeFollower(voteReplay.Term)
					return
				}

				if voteReplay.Term > electionTerm || electionTerm == math.MaxUint64 {
					raft.log("my term is out of dated.")
					raft.makeFollower(voteReplay.Term)
					return
				}

				// lock and check if vote is legit if it legit count
				updateVotes := raft.gotVote(electionTerm, votesReceived, voteReplay)

				// predicate condition to become a leader.
				//  note: if number of peer reduced, we don't care because we know cluster size.
				//        if bunch of node bounced off,  we care about majority.
				raft.mu.Lock()
				votesReceived = updateVotes
				raft.log("collected %d votes", votesReceived)
				if int(votesReceived)*2 > raft.numberPeers+1 {
					raft.log("wins election with %d votes", votesReceived)
					raft.mu.Unlock()
					raft.startLeader()
					return
				}
				raft.mu.Unlock()
			}
		}(peerId)
	}

	// Run another election timer, in case this election is not successful.
	go raft.runElectionTimer()
}

/*
	Method switch a state of node in cluster to a follower
*/
func (raft *RaftProtocol) makeFollower(term uint64) {
	raft.log("Attempting become a follower for term=%d", term)
	raft.mu.Lock()
	raft.log("state change: Become a follower term=%d; log=%v", term, raft.log)
	raft.state = Follower
	raft.currentTerm = term
	raft.votedFor = math.MaxUint64
	raft.electionResetEvent = time.Now()
	raft.mu.Unlock()
	go raft.runElectionTimer()
}

// set leader
func (raft *RaftProtocol) isLeader() bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	if raft.state != Leader {
		return false
	}
	return true
}

// set follower
func (raft *RaftProtocol) isFollower() bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	if raft.state == Follower {
		return true
	}
	return false
}

// set a node a leader
func (raft *RaftProtocol) makeLeader() {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	raft.state = Leader
	raft.log("state change server is Leader for term=%d, log=%v", raft.currentTerm, raft.log)
}

// startLeader switches cm into a leader state and begins process of heartbeats.
// Expects cm.mutex to be locked.
func (raft *RaftProtocol) startLeader() {

	raft.makeLeader()

	// here we can  ( optimization ideas )
	//		a) read from a another channel for event in case we want stop
	// 		b) notify other sub system that we are leader.
	//      c) signal to storage subsystem take snapshoot
	go func() {
		ticker := time.NewTicker(HEARBEAT_INTERVAL * time.Millisecond)
		defer ticker.Stop()

		// Send periodic group broadcast heartbeats, as long we are leader.
		for {
			raft.groupBroadcast()
			<-ticker.C

			// re-check after timeout
			if !raft.isLeader() {
				return
			}
		}
	}()
}

/*
	matchIndex is highest log entry know to be replicated and it increase monotonically
	Method countMatchIndex log entry id for each peer ad return count.
	It doesn't hold a lock,  caller MUST hold a lock
*/
func (raft *RaftProtocol) countMatchIndex(i uint64) int {
	count := 0
	for _, peer := range raft.peers {
		if raft.matchIndex[peer] >= i {
			count++
		}
	}
	return count
}

// return current term
func (raft *RaftProtocol) readCurrentTerm() uint64 {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	return raft.currentTerm
}

/*
	Broadcast msg issue AppendEntriesReply to remote peer.
*/
func (raft *RaftProtocol) broadcastMsg(peer uint64, myTerm uint64) error {

	raft.mu.Lock()
	nextIndex := raft.nextIndex[peer]
	prevLogIndex := nextIndex - 1

	var prevLogTerm uint64 = math.MaxUint64
	if prevLogIndex >= 0 && prevLogIndex < math.MaxUint64 && int(prevLogIndex) < len(raft.stateLog) {
		prevLogTerm = raft.stateLog[prevLogIndex].Term
	}

	myState := raft.state
	entries := raft.stateLog[nextIndex:]
	raft.mu.Unlock()

	appendEntry := &pb.AppendEntries{
		Term:         myTerm,
		LeaderId:     raft.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: raft.volatileState.CommitIndex(),
	}

	// send gRPC call and we DON'T hold a lock.
	raft.log("%v sending append entries -> peer [#%v] ---> [next index=%+v], [leader commit index=%v]",
		color.Red+myState.String()+color.Reset, peer, appendEntry.PrevLogIndex, appendEntry.LeaderCommit)

	rpcReply, err := raft.server.RemoteCall(peer, appendEntry)
	if err != nil {
		glog.Errorf("invalid respond to an rpc call peer "+
			"%v: nextIndex=%d, log index=%+v,  prev log term = [%v], err=[%+v]",
			peer, 0, appendEntry.PrevLogIndex, appendEntry.PrevLogTerm, err)
		return nil
	}

	// check rpc in case we got bogus msg
	replay, ok := rpcReply.(*pb.AppendEntriesReply)
	if !ok {
		glog.Errorf("invalid respond type rpc call peer [#%v] nextIndex=%d, [leader commit index=%v]", peer, 0, appendEntry.LeaderCommit)
		return fmt.Errorf("invalid respond to rpc call")
	}

	// check myTerm if someone already progress, become follower based on myTerm we saw
	if myTerm < replay.Term || myTerm == math.MaxUint64 {
		raft.makeFollower(replay.Term)
		return nil
	}

	raft.mu.Lock()
	defer raft.mu.Unlock()
	// leader and myTerm are matched
	if raft.state == Leader && myTerm == replay.Term {
		if replay.Success {
			raft.nextIndex[peer] = nextIndex + uint64(len(entries))
			raft.matchIndex[peer] = raft.nextIndex[peer] - 1
			raft.log("Append entries rpcReply from [%d] success: nextIndex := [%v], matchIndex := [%v]", peer, raft.nextIndex, raft.matchIndex)
			savedCommitIndex := raft.volatileState.CommitIndex()

			for i := raft.volatileState.CommitIndex() + 1; i < uint64(len(raft.stateLog)); i++ {
				if raft.stateLog[i].Term != raft.currentTerm {
					continue
				}

				matchCount := raft.countMatchIndex(i) + 1
				if matchCount*2 > raft.numberPeers+1 {
					raft.volatileState.setCommitIndex(i)
				}
			}

			if raft.volatileState.CommitIndex() != savedCommitIndex {
				raft.log("%v sets commitIndex := %d", color.Red+myState.String()+color.Reset, raft.volatileState.CommitIndex())
				raft.commitReadyChan <- CommitReady{
					Term: myTerm,
				}
			}
		} else {
			raft.nextIndex[peer] = nextIndex - 1
			raft.log("AppendEntries rpcReply from %d !success: nextIndex := %d", peer, nextIndex-1)
		}
	}

	return nil
}

/*
    Group broadcast periodically sends append message to all
    cluster member.  Each peer connection in separate go routine.

    Each member of cluster must respond,
    Based on respond,  state of current server
    adjusted.

	For example if we see that our term is old, we fall back
    to follower mode.
*/
func (raft *RaftProtocol) groupBroadcast() {

	currentTerm := raft.readCurrentTerm()
	for _, peerId := range raft.peers {
		go func(peer uint64) {
			_ = raft.broadcastMsg(peer, currentTerm)
		}(peerId)
	}
}

/**
 *  Returns a status of current node
 */
func (raft *RaftProtocol) Status() (id uint64, term uint64, isLeader bool) {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	return raft.id, raft.currentTerm, raft.state == Leader
}

/**
Dumps entire log, it mainly for debug purpose.
*/
func (raft *RaftProtocol) getLog() []*pb.LogEntry {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	return raft.stateLog
}

/**
  Submit a log entry as key value pair to log
*/
func (raft *RaftProtocol) Submit(kv *pb.KeyValuePair) bool {
	raft.mu.Lock()
	defer raft.mu.Unlock()

	raft.log("Submit received by %v: [%s]", raft.state, kv.Key)
	if raft.state == Leader {
		raft.stateLog = append(raft.stateLog, &pb.LogEntry{
			Command: kv,
			Term:    raft.currentTerm,
		})
		raft.log("... log=%v", raft.log)
		return true
	}

	return false
}

/**
Shutdown raft sub-system
*/
func (raft *RaftProtocol) Shutdown() {
	raft.mu.Lock()
	defer raft.mu.Unlock()
	raft.state = Dead
	raft.log("Server shutdown.")
	close(raft.commitReadyChan)
}

/**
 	Read from ready channel all msg that committed
	serialize to fun out
*/
func (raft *RaftProtocol) commitChannel() {

	go func() {
		// for each ready msg in the channel
		for range raft.commitReadyChan {
			raft.mu.Lock()
			currentTerm := raft.currentTerm
			oldLastApplied := raft.volatileState.LastApplied()

			var committedEntries []*pb.LogEntry
			if raft.volatileState.CommitIndex() > raft.volatileState.LastApplied() ||
				raft.volatileState.LastApplied() == math.MaxUint64 {
				// set low and high.
				committedEntries = raft.stateLog[raft.volatileState.LastApplied()+1 : raft.volatileState.CommitIndex()+1]
				raft.volatileState.setLastApplied(raft.volatileState.CommitIndex())
			}

			raft.log("committed log committedEntries=%v, lastApplied=%d", committedEntries, oldLastApplied)
			raft.mu.Unlock()

			// for each committed log entry
			for i, e := range committedEntries {
				raft.log("Pushing to a channel commit index=%d", oldLastApplied+uint64(i)+1)
				e.GetCommand()
				raft.commitChan <- CommitEntry{
					Command: KeyValuePair{Key: e.Command.Key, Value: e.Command.Value},
					Index:   oldLastApplied + uint64(i) + 1,
					Term:    currentTerm,
				}
			}
		}
	}()

	raft.log("commitChannel done")
}

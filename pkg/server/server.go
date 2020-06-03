package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	pb "../../api"
	"github.com/golang/glog"
	"google.golang.org/grpc"
)

// TODO add to peer list respond with server spec for auto discovery.

/**
Server spec describe all api binding points
*/
type ServerSpec struct {
	RaftNetworkBind string
	RestNetworkBind string
	GrpcNetworkBind string
	Basedir         string
	LogDir          string
}

/**

 */
type Server struct {

	// server lock, user to provide sync primitive for all internal go routine
	//
	lock sync.Mutex

	// server port
	port string
	// 64 bit hash for server id.  is generate based on ip:port
	serverId uint64
	// string to hold original bind ip:port
	serverBind string
	//
	LastLeaderId uint64
	// last update hold a last rpc message received
	LastUpdate time.Time
	// at each grpc call we stored last term this server saw. note it might outdated
	// but in normal condition it used for reporting.
	LastTerm uint64
	// raft protocol state machine
	raftState *RaftProtocol
	// restful api interface used to by clients
	rest *Restful
	// a GRPC interface by raft protocol and clients
	rpc *grpc.Server
	// a shutdown channel wait for signal, closing this channel will shutdown server
	quit chan interface{}
	// a ready channel,  server initially take time to do initial setup,  a main
	// routine signal a ready signal for raft protocol to beging a state machine.
	// for example if raft protocol issue grpc request while rpc server in init state.
	ready <-chan interface{}
	// wait group used for launch all client go routine and server
	wait sync.WaitGroup

	// hold peers grpc end points
	peers []string

	// a map that store each server bind ip:port and respected server id hash
	peersHash map[string]uint64

	// a map hold all grpc connection to each server, a key is hash id
	peerClient map[uint64]*grpc.ClientConn
	// a slice that hold a server spec,  rest interface bind / base dir / log dir etc
	networkSpec []ServerSpec
	// this mainly to make grpc proto api happy // TODO

	// current server spec
	serverSpec ServerSpec

	// if current server already down , we should reject all calls
	isDead bool

	// storage
	db             Storage
	commitProducer chan CommitEntry

	// proto buffer
	pb.UnimplementedRaftServer
	pb.RequestVote
	pb.AppendEntries
	pb.PingMessage
}

/**
  a GRPC handler for request vote. it receives pb.RequestVote, pack request and apply
  to a raft state.  TODO, in order to decouple proto and internal structure
  we use internal re-presentation but actually it doesn't make sense since proto we already describe
  representation in protobuf
*/
func (s *Server) RequestVoteCall(ctx context.Context, vote *pb.RequestVote) (*pb.RequestVoteReply, error) {

	if s == nil {
		glog.Info("[rpc server handler] server uninitialized and got vote %d", vote.CandidateId)
		return nil, fmt.Errorf("server uninitilized and got vote for %d", vote.CandidateId)
	}

	if s.isDead {
		return nil, fmt.Errorf("server in shutdown state")
	}

	glog.Infof("[rpc server handler] rx vote for a candidate %v", vote.CandidateId)
	//time.Sleep(time.Duration(1 + rand.Intn(5)) * time.Millisecond)

	if s.raftState == nil {
		log.Printf("[rpc server handler] consensus uninitilized.")
		return nil, fmt.Errorf("cm uninitilized. %d", vote.CandidateId)
	}

	rep, err := s.raftState.RequestVote(vote)
	if err != nil {
		log.Printf("[rpc server:] vote failed %v", err)
		return nil, err
	}
	return &pb.RequestVoteReply{
		Term:        rep.Term,
		VoteGranted: rep.VoteGranted,
	}, nil
}

/*
	Return last seen leader
*/
func (s *Server) LastLeader() (*ServerSpec, uint64, bool) {

	for k, v := range s.peersHash {
		if v == s.LastLeaderId {
			for _, spec := range s.networkSpec {
				if spec.RaftNetworkBind == k {
					return &spec, s.LeaderId, true
				}
			}
		}
	}

	//
	return nil, 0, false
}

/**
	A loop that continuously reads from commit channel and submit
    data to storage.  persistent on or memory
*/
func (s *Server) ReadCommits() {

	timeout := make(chan bool, 1)
	// default timeout channel,  in case another end close a channel
	// we will timeout after, 2 second.
	go func() {
		time.Sleep(2 * time.Second)
		timeout <- true
	}()

	var ok = false
	for ok != true {
		select {
		case c := <-s.commitProducer:
			glog.Infof("received from channel commit.", c.Index)
			s.db.Set(c.Command.Key, c.Command.Value)
		case <-timeout:
			glog.Infof("read from channel timeout.")
			ok = true
		}
	}
}

/**

 */
func (s *Server) AppendEntriesCall(ctx context.Context, req *pb.AppendEntries) (*pb.AppendEntriesReply, error) {

	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}

	if s.isDead {
		return nil, fmt.Errorf("server in shutdown state")
	}

	if s == nil {
		glog.Info("[rpc server handler] server uninitialized")
		return nil, fmt.Errorf("server uninitilized")
	}

	//log.Printf("[rpc server handler] recieved")
	//time.Sleep(time.Duration(1 + rand.Intn(5)) * time.Millisecond)
	glog.Infof("[rpc server handler] rx append entries from a leader [%v] term [%v]", req.LeaderId, req.Term)
	rep, err := s.raftState.AppendEntries(req)

	// update last leader seen
	s.LastLeaderId = req.LeaderId
	s.LastUpdate = time.Now()
	s.LastTerm = req.Term

	if err != nil {
		glog.Errorf("[rpc server:] append failed %v", err)
		return nil, err
	}

	glog.Infof("replay for ter, %d state %v", rep.Term, rep.Success)
	return rep, nil
}

///**
//
// */
//func (s *Server) ReadCommits(ctx context.Context) ([]pb.CommitEntry, error) {
//
//	glog.Infof("received read commit request.")
//	if s == nil {
//		return nil, fmt.Errorf("server uninitialized")
//	}
//
//	if s.isDead {
//		return nil, fmt.Errorf("server in shutdown state")
//	}
//
//	if s == nil {
//		glog.Info("[rpc server handler] server uninitialized.")
//		return nil, fmt.Errorf("server uninitilized")
//	}
//
//	commits := s.raftState.ReadCommits()
//	glog.Infof("got respond back, num commits=%d", len(commits))
//
//	return commits, nil
//}

/**

 */
func (s *Server) SubmitCall(ctx context.Context, req *pb.SubmitEntry) (*pb.SubmitReply, error) {

	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}

	if s.isDead {
		return nil, fmt.Errorf("server in shutdown state")
	}

	if s == nil {
		glog.Info("[rpc server handler] server uninitialized.")
		return nil, fmt.Errorf("server uninitilized")
	}

	//key := pb.SubmitEntry.GetCommand().Key
	//val := pb.SubmitEntry.GetCommand().Value
	//s.storage[pb.SubmitEntry] = pb.SubmitEntry.GetCommand().Key

	ok := s.raftState.Submit(req.Command)
	if ok == false {
		glog.Errorf("[rpc server:] submit failed")
		return nil, nil
	}

	glog.Infof("replay for submit req [%v]", ok)

	leaderEndpoint := s.networkSpec[s.LeaderId].RaftNetworkBind

	return &pb.SubmitReply{
		Success:  ok,
		LeaderId: s.LeaderId,
		NodeId:   s.serverId,
		Address:  leaderEndpoint,
	}, nil
}

//
func hash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// generates response to a Ping request
func (s *Server) Ping(ctx context.Context, in *pb.PingMessage) (*pb.PongReply, error) {

	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}

	if s.isDead {
		return nil, fmt.Errorf("server in shutdown state")
	}

	log.Printf("Receive message %s", in.GetName())
	return &pb.PongReply{Message: s.serverBind}, nil
}

/*
  Simulate different cases.  Note that timer depend on
  timeout and leader election timer
*/
func (s *Server) SimulateFailure(req *pb.RequestVote) error {

	if s == nil {
		return fmt.Errorf("server uninitialized")
	}

	simulateProblem := rand.Intn(10)
	if simulateProblem == 1 {
		glog.Infof("drop request vote")
		return fmt.Errorf("rpc failed")
	} else if simulateProblem == 2 {
		glog.Infof("delay request vote")
		time.Sleep(75 * time.Millisecond)
	} else if simulateProblem == 2 {
		glog.Infof("partition 10 second interval")
		time.Sleep(10 * time.Second)
	}

	return nil
}

/*

 */
func (s *Server) ServerID() uint64 {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.serverId
}

/*

 */
func (s *Server) RaftState() *RaftProtocol {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.raftState
}

func (s *Server) GetPeerID(bind string) uint64 {
	return hash(bind)
}

/**
  Create a new server, upon success method returns a pointer to a server.
  - serverSpec must hold local server specs.
  - peers must holds all other peer specs.
  - port is bind address.
*/
func NewServer(serverSpec ServerSpec, peers []ServerSpec, port string, ready <-chan interface{}) (*Server, error) {

	s := new(Server)
	s.serverId = hash(serverSpec.RaftNetworkBind)
	s.serverBind = serverSpec.RaftNetworkBind
	s.serverSpec = serverSpec
	s.isDead = false

	if len(peers) == 0 {
		log.Fatal("Error", len(peers))
	}

	glog.Infof("[rpc server handler] Server %v id %v", serverSpec.RaftNetworkBind, s.serverId)

	s.peersHash = make(map[string]uint64)
	s.peerClient = make(map[uint64]*grpc.ClientConn)

	s.networkSpec = peers

	// for each server we generate hash
	for _, spec := range peers {
		h := hash(spec.RaftNetworkBind)
		glog.Infof("Server peers %v id %v", spec.RaftNetworkBind, h)
		s.peersHash[spec.RaftNetworkBind] = hash(spec.RaftNetworkBind)
	}

	s.port = port
	s.ready = ready
	s.quit = make(chan interface{})

	var err error
	s.commitProducer = make(chan CommitEntry, 32)

	s.raftState, err = NewRaftProtocol(s.serverId, s.peersHash, s, len(peers), s.ready, s.commitProducer)
	if err != nil {
		return nil, err
	}

	s.rest, err = NewRestfulServer(s, serverSpec.RestNetworkBind, serverSpec.Basedir)

	go s.ReadCommits()

	return s, err
}

/**
    Start server a new server, upon success method returns a pointer to a server.
  - serverSpec must hold local server specs.
  - peers must holds all other peer specs.
  - port is bind address.
*/
func (s *Server) Start() error {

	s.lock.Lock()
	defer s.lock.Unlock()
	// if not dead nothing to do
	if !s.isDead {
		return nil
	}

	ready := make(chan interface{})
	s.ready = ready

	// re-create quit channel
	s.quit = make(chan interface{})

	var err error
	// create new raft module
	commitChans := make(chan CommitEntry)
	s.raftState, err = NewRaftProtocol(s.serverId, s.peersHash, s, len(s.peers), s.ready, commitChans)
	if err != nil {
		return err
	}

	var entries []CommitEntry

	for c := range commitChans {
		//		pong(raft.commitChan, pongs)
		entries = append(entries, c)
	}

	//	s.rest, err = NewRestfulServer(s, s.serverSpec.RestNetworkBind, serverSpec.Basedir)
	s.isDead = false

	// close ready channel and signal to raft to start fsm
	// restart all rpc service and signal to raft
	err = s.Serve()
	if err != err {
		return err
	}

	close(ready)

	return nil
}

/**
  Create a listener used by grpc, rocinante uses tcp to communicate
  between peers
*/
func (s *Server) createListener() (net.Listener, error) {

	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}

	if s.isDead {
		return nil, fmt.Errorf("server in shutdown state")
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	lis, err := net.Listen("tcp", s.serverBind)
	if err != nil {
		return nil, err
	}

	// creates a gRPC server
	s.rpc = grpc.NewServer()
	pb.RegisterRaftServer(s.rpc, s)
	return lis, nil
}

/**
  Creates all peer connection between all node in cluster.
  Each connection in a separate go routine and each connection added to peer list.

  Method create GRPC listener and bind to protobuf to it
  At the method will block and wait for a signal to shutdown.
*/
func (s *Server) Serve() error {

	if s == nil {
		return fmt.Errorf("server uninitialized")
	}

	if s.isDead {
		return fmt.Errorf("server in shutdown state")
	}

	lis, err := s.createListener()
	if err != nil {
		glog.Fatalf("failed create listener: %v", err)
		return err
	}

	s.wait.Add(1)
	go func() {
		glog.Infof("[rpc server] Server started.")
		defer s.wait.Done()

		for _, peerId := range s.networkSpec {
			// open connection to each peer in separate thread and block
			go func(p ServerSpec) {
				glog.Infof("Attempting connect to a peer %v", p.RaftNetworkBind)
				conn, err := grpc.Dial(p.RaftNetworkBind, grpc.WithInsecure(), grpc.WithBlock())
				if err != nil {
					glog.Fatalf("failed to connect to a peer: %v", err)
				}
				glog.Infof("Connected to %v", p.RaftNetworkBind)

				// add to map all connection handlers to server
				s.lock.Lock()
				s.peerClient[hash(p.RaftNetworkBind)] = conn
				s.lock.Unlock()

			}(peerId)
		}

		// server added to wait group
		s.wait.Add(1)
		go func() {
			if err := s.rpc.Serve(lis); err != nil {
				log.Fatalf("[rpc server] failed serve: %v", err)
			}
			s.wait.Done()
		}()
	}()

	log.Printf("Server started.")
	<-s.quit
	s.wait.Wait()

	log.Printf("Shutdown")

	return nil
}

/*
   Returns a peer connection based on node id.
*/
func (s *Server) getPeer(peerID uint64) (*grpc.ClientConn, bool) {

	if s == nil {
		return nil, false
	}

	if s.isDead {
		return nil, false
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	peer, ok := s.peerClient[peerID]
	return peer, ok
}

/*
   Execute remote call,  Method provide generate placeholder for a transport.
   TODO add interface and plugable transport.

   The idea here we want be able provide alternative option for node to communicate.
   For example if we decide to choose later alternative protocol.
*/
func (s *Server) RemoteCall(peerID uint64, args interface{}) (interface{}, error) {

	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}

	if s.isDead {
		return nil, fmt.Errorf("server in shutdown state")
	}

	if peer, ok := s.getPeer(peerID); ok {
		c := pb.NewRaftClient(peer)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Request Vote
		if req, ok := args.(*pb.RequestVote); ok {
			vote := &pb.RequestVote{
				Term:         req.Term,
				CandidateId:  req.CandidateId,
				LastLogIndex: req.LastLogIndex,
				LastLogTerm:  req.LastLogTerm,
			}
			// send vote
			glog.Infof("[rpc server] sending rpc vote msg : my id %v to %v\n", s.serverBind, peer.Target())
			replay, err := c.RequestVoteCall(ctx, vote)
			if err != nil {
				glog.Error("[rpc server] cm returned error %v", err)
				return nil, err
			}
			var rr RequestVoteReply
			if replay != nil {
				rr.Term = replay.Term
				rr.VoteGranted = replay.VoteGranted
			} else {
				return nil, fmt.Errorf("wrong vote replay message")
			}

			return &rr, nil
		} else if req, ok := args.(*pb.AppendEntries); ok {
			entry := &pb.AppendEntries{
				Term:         req.Term,
				LeaderId:     req.LeaderId,
				PrevLogIndex: req.PrevLogIndex,
				PrevLogTerm:  req.PrevLogTerm,
				Entries:      req.Entries,
				LeaderCommit: req.LeaderCommit,
			}
			glog.Infof("[rpc server] sending rpc [append entries msg] : id %v to %v\n", s.serverBind, peer.Target())
			return c.AppendEntriesCall(ctx, entry)
		} else {
			glog.Errorf("unknown message %T", args)
		}
	} else {
		glog.Errorf("Peer %d not found", peerID)
	}

	return nil, fmt.Errorf("connect to %d peer closed", peerID)
}

//
func (s *Server) RESTEndpoint() ServerSpec {
	if s == nil {
		return ServerSpec{}
	}
	return s.serverSpec
}

/*
   Shutdown a server and gracefully signal to RAFT protocol
   to shutdown
*/
func (s *Server) Shutdown() {

	if s == nil {
		return
	}

	s.lock.Lock()
	if s.isDead {
		s.lock.Unlock()
		return
	}

	s.isDead = true
	s.lock.Unlock()
	glog.Infof("server going to shutdown state")
	s.raftState.Shutdown()
	s.rpc.Stop()
	close(s.quit)
}

//
func (s *Server) PeerStatus() map[uint64]string {

	connStatus := make(map[uint64]string)

	for k, v := range s.peerClient {
		connStatus[k] = v.GetState().String()
	}

	return connStatus
}

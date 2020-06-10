package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	pb "../../api"
	"../../pkg/color"
	"../io"
	"github.com/golang/glog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// TODO add to peer list respond with server spec for auto discovery.

type ServiceState int

var ServerDisconnected = errors.New("server disconnected or init")

const (
	Init ServiceState = iota
	Running
	Disconnected
	Shutdown
)

/**
Server spec describe all api binding points
*/
type ServerSpec struct {
	// server id generate during start up, and it based on ip:port
	ServerID uint64
	// raft network bind ip:port
	RaftNetworkBind string
	// rest service bind ip:port
	RestNetworkBind string
	// grpc services bind ip:port
	GrpcNetworkBind string
	// base dir for a server. web template , logs etc
	Basedir string
	// base where to spool logs
	LogDir string
}

/*
	API endpoint for clients
*/
type ApiEndpoints struct {
	// raft api end point  ip:port
	RaftNetworkBind string
	// rest api end point ip:port
	RestNetworkBind string
	// grpc client side end point ip:port
	GrpcNetworkBind string
}

type PeerStatus struct {
	// api endpoint
	Endpoints ApiEndpoints
	// connectivity state based on grpc  idle, connected, ready etc
	State connectivity.State
}

/**

 */
type Server struct {

	// server lock, user to provide sync primitive for all internal go routine
	//
	lock sync.Mutex
	//
	serverState ServiceState
	// server port
	port string
	// 64 bit hash for server id.  is generate based on ip:port
	serverId uint64
	// string to hold original bind ip:port
	serverBind string
	// last leader id seen
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
	// a shutdownGrpc channel wait for signal, closing this channel will shutdownGrpc server
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

	// a slice that hold a peer server spec,  rest interface bind / base dir / log dir etc
	peerSpec []ServerSpec

	// current server spec
	serverSpec ServerSpec

	// if current server already down , we should reject all calls
	//isDead bool

	// first storage store key value commit
	db Storage
	// second storage key value mapping to index
	committedLog Storage

	commitProducer chan CommitEntry

	// rest ready channel
	restReady chan bool

	// grpc listnner
	listener *net.Listener

	restOn bool

	submitRx int

	voteRx   int
	voteTx   int
	appendRx int
	appendTx int

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

	if s.serverState == Shutdown {
		return nil, fmt.Errorf("server in shutdownGrpc state")
	}

	if s.serverState == Init || s.serverState == Disconnected {
		return nil, fmt.Errorf("server in init or disconnected")
	}

	s.voteRx++

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

	if s == nil {
		return nil, 0, false
	}

	for k, v := range s.peersHash {
		if v == s.LastLeaderId {
			for _, spec := range s.peerSpec {
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
	A commit loop that continuously reads from commit channel and SubmitKeyValue
    data to in memory or persistent storage.  persistent on or memory
*/
func (s *Server) ReadCommits() {

	timeout := make(chan bool, 1)
	// default timeout channel,  in case another end close a channel
	// we will timeout after, 2 second.
	go func() {
		time.Sleep(2 * time.Second)
		timeout <- true
	}()

	//var ok = false
	for {
		select {
		case c := <-s.commitProducer:
			msg := color.Green + "received from channel commit index" + color.Reset
			glog.Infof("%s %d %v %d", msg, c.Index, c.Command.Key, s.db.Size())

			// store and check.
			s.db.Set(c.Command.Key, c.Command.Value)

			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, c.Index)
			s.committedLog.Set(c.Command.Key, buf)

			// case <-timeout:
			//	glog.Infof("read from channel timeout.")
			//	ok = true
			//}
		}
	}
}

/**
A gRPC handler used by proto buffer , triggered upon
Append Entry call
*/
func (s *Server) AppendEntriesCall(ctx context.Context, req *pb.AppendEntries) (*pb.AppendEntriesReply, error) {

	if s == nil {
		glog.Info("[rpc server handler] server uninitialized")
		return nil, fmt.Errorf("server uninitilized")
	}

	if s.serverState == Shutdown {
		return nil, fmt.Errorf("server in shutdownGrpc state")
	}

	if s.serverState == Init || s.serverState == Disconnected {
		return nil, fmt.Errorf("server in init or disconnected")
	}

	s.appendRx++

	glog.Infof("[rpc server handler] rx append entries from a leader [%v] term [%v]", req.LeaderId, req.Term)

	//d := time.Now().Add(50 * time.Millisecond)
	//ctx, cancel := context.WithDeadline(context.Background(), d)
	//
	//defer cancel()
	//
	//select {
	//	case <-time.After(1 * time.Second):
	//		fmt.Println("overslept")
	//case <-ctx.Done():
	//		fmt.Println(ctx.Err())
	//default:
	//}

	rep, err := s.raftState.AppendEntries(req)

	// update last leader seen
	s.lock.Lock()
	s.LastLeaderId = req.LeaderId
	s.LastUpdate = time.Now()
	s.LastTerm = req.Term
	s.lock.Unlock()

	if err != nil {
		glog.Errorf("[rpc server:] append failed %v", err)
		return nil, err
	}

	glog.Infof("replay for ter, %d state %v", rep.Term, rep.Success)
	return rep, nil
}

/**
return result for get value
*/
type GetValueRespond struct {
	Val     []byte
	Index   uint64
	Success bool
}

/**
 */
func (s *Server) GetValue(ctx context.Context, key string) (*GetValueRespond, error) {

	glog.Infof("received read commit request.")
	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}
	if s.serverState == Shutdown {
		return nil, fmt.Errorf("server in shutdownGrpc state")
	}
	if s.serverState == Init || s.serverState == Disconnected {
		return nil, fmt.Errorf("server in init or disconnected")
	}

	var resp GetValueRespond
	valOk := false
	select {
	case v, _ := <-s.db.GetAsync(key):
		resp.Val = v.Val
		valOk = v.Success
	case <-ctx.Done():
		return &resp, ctx.Err()
	}

	indexOk := false
	select {
	case index, _ := <-s.committedLog.GetAsync(key):
		resp.Index = io.ReadUint64(index.Val)
		indexOk = index.Success
	case <-ctx.Done():
		return &resp, ctx.Err()
	}

	if valOk == true && indexOk == true {
		resp.Success = true
		return &resp, nil
	}

	return &resp, nil

	//index := s.committedLog.GetAsync(key)
	//
	//if v. && logOk {
	//return v, ReadUint64(index), true, nil
	//if v. && logOk {
	//return v, ReadUint64(index), true, nil

}

/**

 */
func (s *Server) SubmitCall(ctx context.Context, req *pb.SubmitEntry) (*pb.SubmitReply, error) {

	s.submitRx++

	if s == nil {
		return nil, fmt.Errorf("server uninitialized")
	}

	if s.serverState == Shutdown {
		return nil, fmt.Errorf("server in shutdownGrpc state")
	}

	if s.serverState == Init || s.serverState == Disconnected {
		return nil, fmt.Errorf("server in init or disconnected")
	}

	if s == nil {
		glog.Info("[rpc server handler] server uninitialized.")
		return nil, fmt.Errorf("server uninitilized")
	}

	ok := s.raftState.Submit(req.Command)
	if ok == false {
		glog.Errorf("[rpc server:] SubmitKeyValue failed")
		return nil, nil
	}

	glog.Infof("replay for SubmitKeyValue req [%v]", ok)

	leaderEndpoint := s.peerSpec[s.LeaderId].RaftNetworkBind

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

	if s.serverState == Shutdown {
		return nil, fmt.Errorf("server in shutdownGrpc state")
	}

	if s.serverState == Init || s.serverState == Disconnected {
		return nil, fmt.Errorf("server in init or disconnected")
	}

	log.Printf("Receive message %s", in.GetName())
	return &pb.PongReply{Message: s.serverBind}, nil
}

/*
  Simulate different cases.  Note that timer depend on
  timeout and leader election timer
*/
func (s *Server) SimulateFailure() error {

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
  Return this server id
*/
func (s *Server) ServerID() uint64 {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.serverId
}

/*
   Return a point to Raft Protocol State, it used for unit test only.
*/
func (s *Server) RaftState() *RaftProtocol {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.raftState
}

/*
	Return this peer id, as 64 bit hash
*/
func (s *Server) GetPeerID(bind string) uint64 {
	return hash(bind)
}

/**
  Create a new server, upon success method returns a pointer to a server.
  - serverSpec must hold local server specs.
  - peers must holds all other peer specs.
  - port is bind address.
*/
func NewServer(spec ServerSpec, peers []ServerSpec, port string, restOn bool, ready <-chan interface{}) (*Server, error) {

	s := new(Server)
	// storage for key value
	s.db = NewVolatileStorage()
	// storage that hold key - commit index
	s.committedLog = NewVolatileStorage()
	// server hash
	s.serverId = hash(spec.RaftNetworkBind)
	spec.ServerID = s.serverId
	s.serverBind = spec.RaftNetworkBind
	s.serverSpec = spec
	s.serverState = Init
	s.restOn = restOn

	if len(peers) == 0 {
		log.Fatal("Error", len(peers))
	}

	glog.Infof("[rpc server handler] Server %v id %v", spec.RaftNetworkBind, s.serverId)

	s.peersHash = make(map[string]uint64)
	s.peerClient = make(map[uint64]*grpc.ClientConn)

	s.peerSpec = peers

	// for each server we generate hash that form server id
	for i, spec := range peers {
		peerHash := hash(spec.RaftNetworkBind)
		glog.Infof("Server peers %v id %v", spec.RaftNetworkBind, peerHash)
		s.peersHash[spec.RaftNetworkBind] = peerHash
		s.peerSpec[i].ServerID = peerHash
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

	s.restReady = make(chan bool)
	s.rest, err = NewRestfulServer(s, s.serverSpec.RestNetworkBind, s.serverSpec.Basedir, s.restReady)
	if err != nil {
		return nil, err
	}

	if s.rest == nil {
		return nil, fmt.Errorf("failed to start rest api service")
	}

	//
	go s.ReadCommits()

	return s, err
}

/**

 */
func (s *Server) StartRest() {

	go func() {
		s.rest.Serve()
	}()
	// wait for server tell it ready
	<-s.restReady
}

/**

 */
func (s *Server) StopRest() {
	s.rest.shutdownRest()
}

/**
    Start a new server, upon success method returns a pointer to a server.
  - serverSpec must hold local server specs.
  - peers must holds all other peer specs.
  - port is bind address.
*/
func (s *Server) Start() error {

	s.lock.Lock()
	defer s.lock.Unlock()
	// if not dead nothing to do
	if s.serverState == Running {
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
		entries = append(entries, c)
	}

	//	s.rest, err = NewRestfulServer(s, s.serverSpec.RestNetworkBind, serverSpec.Basedir)
	s.serverState = Running

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

	if s.serverState == Shutdown || s.serverState == Disconnected {
		return nil, fmt.Errorf("server in init or disconnected")
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
  At the method will block and wait for a signal to shutdownGrpc.
*/
func (s *Server) Serve() error {

	if s == nil {
		return fmt.Errorf("server uninitialized")
	}

	if s.serverState == Shutdown {
		return fmt.Errorf("server in shutdownGrpc state")
	}

	if s.serverState == Running || s.serverState == Disconnected {
		return fmt.Errorf("server in init or disconnected")
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

		for _, peerId := range s.peerSpec {
			// open connection to each peer in separate thread and block
			go func(p ServerSpec) {
				// if already open
				if c, ok := s.peerClient[hash(p.RaftNetworkBind)]; ok {
					_ = c.Close()
				}
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

		s.lock.Lock()
		s.serverState = Running
		s.lock.Unlock()

		// server added to wait group
		s.wait.Add(1)
		go func() {
			if err := s.rpc.Serve(lis); err != nil {
				log.Fatalf("[rpc server] failed serve: %v", err)
			}
			s.lock.Lock()
			defer s.lock.Unlock()
			s.listener = &lis
			s.wait.Done()
		}()
	}()

	log.Printf("Server started.")

	s.StartRest()

	<-s.quit
	s.wait.Wait()

	log.Printf("Shutdown")
	lis.Close()

	return nil
}

/*
   Returns a peer grpc connection based on node id.
*/
func (s *Server) getPeer(peerID uint64) (*grpc.ClientConn, bool) {

	if s == nil {
		return nil, false
	}

	if s.serverState == Shutdown || s.serverState == Init {
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

	if s.serverState == Shutdown {
		return nil, fmt.Errorf("server in shutdownGrpc state")
	}
	if s.serverState == Init || s.serverState == Disconnected {
		return nil, ServerDisconnected
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
				glog.Error("[rpc server] cm returned error ", err)
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
		glog.Errorf(" Peer %d not found, probably disconnected.", peerID)
	}

	return nil, fmt.Errorf("connect to %d peer closed", peerID)
}

/*
  Returns current server rest api end point address
*/
func (s *Server) RESTEndpoint() ServerSpec {
	if s == nil {
		return ServerSpec{}
	}
	return s.serverSpec
}

/*
   Shutdown a server and gracefully signal to RAFT protocol
   to shutdownGrpc
*/
func (s *Server) Shutdown() {

	if s == nil {
		return
	}

	s.lock.Lock()
	if s.serverState == Shutdown {
		glog.Infof("Changed server state to ", s.serverState)
		s.lock.Unlock()
		return
	}

	s.serverState = Shutdown

	if s.rest != nil {
		// shutdown rest
		glog.Infof("Sending signal to rest server to shutdown. %v", s.serverState)
		s.rest.shutdownRest()
		//s.rest.StopRest()
		for r := 0; r < 3; r++ {
			released := 0
			for released <= 1 {
				if io.CheckSocket(s.RESTEndpoint().RestNetworkBind, "tcp") {
					glog.Infof("server released port %v", s.ServerBind())
					released++
					break
				}
			}
			if released == 1 {
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	s.lock.Unlock()
	glog.Infof("server going to shutdown grpc state")
	// shutdownGrpc raft state
	if s.raftState != nil {
		s.raftState.Shutdown()
	}
	// close all peer connection
	glog.Infof("closing all peer connections")
	for _, p := range s.peerClient {
		if p != nil {
			_ = p.Close()
		}
	}
	s.peerClient = make(map[uint64]*grpc.ClientConn)

	// stop grpc server
	s.rpc.GracefulStop()
	// close quit channel
	if s.quit != nil {
		close(s.quit)
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	if s.listener != nil {
		(*s.listener).Close()
	}
}

func (s *Server) ShutdownGrpc() {

	if s == nil {
		return
	}

	s.lock.Lock()
	if s.serverState == Shutdown {
		glog.Infof("Changed server state to ", s.serverState)
		s.lock.Unlock()
		return
	}

	s.serverState = Shutdown

	s.lock.Unlock()
	glog.Infof("server going to shutdown grpc state")

	// close all peer connection
	glog.Infof("closing all peer connections")
	for _, p := range s.peerClient {
		if p != nil {
			_ = p.Close()
		}
	}
	s.peerClient = make(map[uint64]*grpc.ClientConn)

	// stop grpc server
	s.rpc.GracefulStop()
	// close quit channel
}

/**
  Returns in memory storage, it mainly used for unit testing.
*/
func (s *Server) GetInMemoryStorage() map[string][]byte {
	return s.db.GetCopy()
}

/**
  Return a peer spec, if peer not found error
*/
func (s *Server) getPeerSpec(id uint64) (*ServerSpec, error) {

	for _, v := range s.peerSpec {
		if v.ServerID == id {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("peer not found")
}

/**
Return server tcp bind
*/
func (s *Server) ServerBind() string {
	return s.serverBind
}

/**
  Returns peer status as map, a key is host id of each server
  value a is peer spec
*/
func (s *Server) PeerStatus() map[uint64]PeerStatus {

	connStatus := make(map[uint64]PeerStatus)
	if s == nil {
		return connStatus
	}

	for k, v := range s.peerClient {
		peer, err := s.getPeerSpec(k)
		if err != nil {
			log.Fatal("incorrect state.")
		}

		connStatus[k] = PeerStatus{
			Endpoints: ApiEndpoints{
				RaftNetworkBind: peer.RaftNetworkBind,
				RestNetworkBind: peer.RestNetworkBind,
				GrpcNetworkBind: peer.GrpcNetworkBind,
			},
			State: v.GetState(),
		}
	}

	connStatus[s.ServerID()] = PeerStatus{
		Endpoints: ApiEndpoints{
			RaftNetworkBind: s.serverSpec.RaftNetworkBind,
			RestNetworkBind: s.serverSpec.RestNetworkBind,
			GrpcNetworkBind: s.serverSpec.GrpcNetworkBind,
		},
		State: connectivity.Ready,
	}

	return connStatus
}

/*
   Disconnect from all peers,
   during disconnected state, server will not accept incoming
   request.
*/
func (s *Server) Disconnect() {

	if s == nil {
		return
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	if s.serverState == Running {
		s.serverState = Disconnected
		glog.Infof("server disconnected")
		// close all peer connection
		for i, _ := range s.peerClient {
			_ = s.peerClient[i].Close()
			//ctx, cancel := context.WithTimeout(context.Background(), 50 * time.Millisecond)
			//shutdownCh := connectionOnState(ctx, s.peerClient[i], connectivity.Shutdown)
			//<-shutdownCh
			//cancel()
		}

		s.peerClient = make(map[uint64]*grpc.ClientConn)
	}
}

/**

 */
func (s *Server) Reconnect() {

	var wait sync.WaitGroup
	for _, peerId := range s.peerSpec {

		wait.Add(1)
		// open connection to each peer in separate thread and block
		go func(p ServerSpec) {
			defer wait.Done()

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

	wait.Wait()
	s.serverState = Running
}

// return server id, term id, and leader or not
func (s *Server) IsLeader() (uint64, uint64, bool) {
	return s.raftState.Status()
}

// return true if current status running or not
func (s *Server) IsRunning() bool {
	return s.serverState == Running
}

// return true if disconnected or not
func (s *Server) IsDisconnected() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.serverState == Disconnected
}

func (s *Server) IsShutdown() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.serverState == Shutdown
}

/**
Sets connection status for GRPC peer,
we can use it to set explicitly status
*/
func connectionOnState(ctx context.Context, conn *grpc.ClientConn, states ...connectivity.State) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		// continue checking for state change
		// until one of break states is found
		for {
			change := conn.WaitForStateChange(ctx, conn.GetState())
			if !change {
				// ctx is done, return
				// something upstream is cancelling
				return
			}

			currentState := conn.GetState()

			for _, s := range states {
				if currentState == s {
					// matches one of the states passed
					// return, closing the done channel
					return
				}
			}
		}
	}()

	return done
}

func (s *Server) startStatCollector() {

	if s == nil {
		return
	}

	//	oldVote := 0

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		// Send periodic group broadcast heartbeats, as long we are leader.
		for {
			<-ticker.C
			//delta := s.voteRx
			//s.voteRx = 0
		}
	}()
}

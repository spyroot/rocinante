package server

import (
	"bytes"
	"context"
	b64 "encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	pb "github.com/spyroot/rocinante/api"
	"github.com/spyroot/rocinante/pkg/flow"
	"github.com/spyroot/rocinante/pkg/io"

	"github.com/golang/glog"
	"github.com/gorilla/mux"
	weblogger "github.com/sirupsen/logrus"
)

const DefaultHttpTimeout time.Duration = 250

const ApiTransport string = "http://"
const ApiLeader string = "/leader"
const ApiSubmit string = "/submit"
const ApiShutdown string = "/shutdownGrpc"
const ApiShutdownNode string = "/shutdownNode/{node}"

const ApiLog string = "/log"
const ApiCommitted string = "/committed"
const ApiGet string = "/get"
const ApiFlows string = "/flows/{size}/{id:[0-9]+}"
const ApiPeerList = "/peer/list"
const ApiIndex = "/"
const ApiFlowIndex = "/flow"
const ApiRole = "/role"
const DefaultLogSize int = 5

type Restful struct {
	// server mutex
	lock sync.Mutex
	// a pointer to instantiated rest server.
	server *Server
	// a pointer to http server
	restServer *http.Server
	// base dir where all template , logs
	basedir string
	// a channel that we wait for shutdownGrpc
	shutdownRequest chan bool
	//
	shutdownReqCount uint32
	// ready
	ready chan<- bool

	// healthy monitoring for rest
	healthy int32
}

type Peer struct {
	ServerNei string
	ServerID  string
}

type Info struct {
	ServerBind   string
	ServerID     string
	Role         string
	Term         uint64
	LastUpdate   string
	LastElection string
	Connected    []Peer
}

type PageData struct {
	ServerID     string
	ServerStatus []Info
	NumPeers     uint64
}

type LeaderRespond struct {
	// leader id
	Leader uint64 `json:"leader"`
	// true if this server is leader
	Success bool `json:"success"`
	// grpc api client binding
	GrpcBinding string `json:"GrpcBinding"`
	// rest api client binding
	RestBinding string `json:"RestBinding"`
}

type LogRespondEntry struct {
	// leader id
	Key    string `json:"key"`
	Value  string `json:"value"`
	Term   uint64 `json:"term"`
	Synced bool
}

type LogRespond struct {
	// leader id
	Last_page int               `json:"last_page"`
	Data      []LogRespondEntry `json:"data"`
}

type HttpValueRespond struct {
	// leader id
	Value   []byte `json:"value"`
	Success bool   `json:"success"`
}

/*
	Router handler for restful API
*/
func NewRestfulServer(s *Server, bind string, base string, ready chan<- bool) (*Restful, error) {

	r := new(Restful)
	r.server = s
	glog.Infof("Rest server base dir %s", base)
	r.basedir = base

	ok, err := io.IsDir(base)
	if err != nil {
		glog.Errorf("base dir %s is not valid directory", err)
		close(ready)
		return nil, err
	}

	if !ok {
		glog.Errorf("base dir %s is not valid directory", base)
		close(ready)
		return nil, fmt.Errorf("base dir %s is not valid directory", base)
	}

	// register all end points
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc(ApiIndex, r.HandlerIndex)
	router.HandleFunc("/submit/{key}/{val}", r.SubmitKeyValue)
	router.HandleFunc(ApiCommitted, r.getCommitted)
	router.HandleFunc(ApiGet+"/{key}", r.getValue)
	router.HandleFunc(ApiLog, r.getLog)
	router.HandleFunc(ApiShutdown, r.shutdownGrpc)
	router.HandleFunc(ApiLeader, r.leader)
	router.HandleFunc(ApiPeerList, r.HandlerPeerList)
	router.HandleFunc(ApiFlows, r.HandlerFlows)
	router.HandleFunc("/size", r.HandlerCommitedSize)
	router.HandleFunc(ApiFlowIndex, r.HandleFlowIndex)
	router.HandleFunc(ApiRole, r.HandlerRole)
	router.HandleFunc(ApiShutdownNode, r.HandlerNodeShutdown)

	// template css
	ss := http.StripPrefix("/template/", http.FileServer(http.Dir(base+"/pkg/template/")))
	router.PathPrefix("/template/").Handler(ss)

	glog.Infof("[restful server started]: %s", bind)

	r.restServer = &http.Server{
		Handler:      router,
		Addr:         bind,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	//	var log = logrus.New()
	weblogger.SetFormatter(&weblogger.JSONFormatter{})
	logFile := base + "/" + bind + "restapi.log"
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0666)
	if err == nil {
		weblogger.SetOutput(file)
	} else {
		glog.Info("Failed to log to file, using default stderr")
	}

	standardFields := weblogger.Fields{
		"hostname": "staging-1",
		"appname":  "foo-app",
		"session":  "1ce3f6v",
	}

	weblogger.WithFields(standardFields).WithFields(weblogger.Fields{
		"string": r.restServer.Addr,
		"int":    1,
		"float":  1.1}).Info("server started")

	r.ready = ready
	return r, nil
}

func (rest *Restful) Serve() {

	done := make(chan bool)
	go func() {
		atomic.StoreInt32(&rest.healthy, 1)
		err := rest.restServer.ListenAndServe()
		if err != nil {
			glog.Errorf("Listen and serve %v", err)
		}
		done <- true
	}()

	rest.ready <- true
	glog.Infof("rest service started. ")

	// wait for shutdown
	rest.WaitShutdown()
	<-done
}

/**

 */
func (rest *Restful) HandlerCommitedSize(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "size, %d!", rest.server.db.Size())
}

/**

 */
func (rest *Restful) HandlerPeeStatus(p string) (*Info, error) {

	// TODO fix
	req, err := http.NewRequest("GET", "http://"+p+ApiRole, nil)
	if err != nil {
		weblogger.Errorf("Error server unavailable %v", err)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultHttpTimeout*time.Millisecond)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		weblogger.Errorf("Error request timeout. Retrying next peer %v", err)
		return nil, err
	}

	var peer Info
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&peer)
	if err != nil {
		glog.Infof("Failed decode respond", err)
		return nil, err
	}

	return &peer, nil
}

/*
	Index page for rest server.
*/
func (rest *Restful) HandlerIndex(w http.ResponseWriter, r *http.Request) {

	templateFile := filepath.Join(rest.basedir, "pkg/template/layout.html")
	tmpl := template.Must(template.ParseFiles(templateFile))

	term, state, lastElection := rest.server.raftState.getState()
	//term := rest.server.raftState.getTerm()
	lastUpdate := rest.server.LastUpdate
	//lastElection := rest.server.raftState.electionResetEvent

	updated := fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d 00:00\n",
		lastUpdate.Year(), lastUpdate.Month(), lastUpdate.Day(),
		lastUpdate.Hour(), lastUpdate.Minute(), lastUpdate.Second())

	election := fmt.Sprintf("%02d:%02d:%02d 00:00\n",
		lastElection.Hour(), lastElection.Minute(), lastElection.Second())

	data := PageData{ServerID: rest.server.serverBind}
	data.NumPeers = uint64(len(rest.server.peers))

	currentServer := Info{
		ServerBind:   rest.server.serverSpec.RaftNetworkBind,
		ServerID:     strconv.FormatUint(rest.server.serverId, 10),
		Role:         state.String(),
		Term:         term,
		LastUpdate:   updated,
		LastElection: election,
	}

	data.ServerStatus = append(data.ServerStatus, currentServer)
	var prev = rest.server.serverSpec.RaftNetworkBind
	var prevID = currentServer.ServerID

	for _, p := range rest.server.peerSpec {

		pstat := "Dead"
		pterm := uint64(0)
		pupdate := ""
		pelecte := ""

		peerStatus, err := rest.HandlerPeeStatus(p.RestNetworkBind)
		if err == nil {
			pstat = peerStatus.Role
			pterm = peerStatus.Term
			pupdate = peerStatus.LastUpdate
			pelecte = peerStatus.LastElection
		}

		currentPeerId := strconv.FormatUint(rest.server.GetPeerID(p.RaftNetworkBind), 10)
		cur := Info{
			ServerBind:   p.RaftNetworkBind,
			ServerID:     currentPeerId,
			Role:         pstat,
			Term:         pterm,
			LastUpdate:   pupdate,
			LastElection: pelecte}

		if pstat != "Dead" {
			cur.Connected = append(cur.Connected, Peer{
				ServerNei: prev,
				ServerID:  prevID})
		}

		data.ServerStatus = append(data.ServerStatus, cur)
		if pstat != "Dead" {
			prev = p.RaftNetworkBind
			prevID = currentPeerId
		}
	}

	svp := Peer{
		ServerNei: prev,
		ServerID:  prevID,
	}

	data.ServerStatus[0].Connected = append(data.ServerStatus[0].Connected, svp)
	err := tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

/**

 */
func (rest *Restful) leader(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		return
	}
	leaderId, _, ok := rest.server.RaftState().Status()

	var resp LeaderRespond
	// if this server is leader respond back
	if ok {

		resp.Leader = leaderId
		resp.Success = true
		resp.GrpcBinding = rest.server.serverSpec.GrpcNetworkBind
		resp.RestBinding = rest.server.serverSpec.RestNetworkBind

		// respond
		w.Header().Set("Content-Type", "application/json")
		js, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(js)
		return
	}

	// if not get id of last know leader and respond back.
	// leader can be partition but it best we could do
	if rest.server == nil {
		return
	}

	spec, vip, ok := rest.server.LastLeader()
	if ok == false {
		glog.Infof("Can't find a leader")
		return
	}

	resp.Success = false
	resp.Leader = vip
	resp.GrpcBinding = spec.GrpcNetworkBind
	resp.RestBinding = spec.RestNetworkBind

	w.Header().Set("Content-Type", "application/json")
	js, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(js)
}

/**

 */
func (rest *Restful) HandlerShutdownRemote(p string) (int, error) {

	// TODO fix
	req, err := http.NewRequest("GET", "http://"+p+ApiShutdown, nil)
	if err != nil {
		weblogger.Errorf("Error server unavailable %v", err)
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultHttpTimeout*time.Millisecond)
	defer cancel()
	r, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		weblogger.Errorf("Error request timeout. Retrying next peer %v", err)
		return 0, err
	}

	return r.StatusCode, nil
}

func (rest *Restful) HandlerNodeShutdown(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	rest.lock.Lock()
	defer rest.lock.Unlock()

	vars := mux.Vars(r)
	n := vars["node"]

	if len(n) == 0 {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	if n == rest.server.serverSpec.RaftNetworkBind {
		rest.server.ShutdownGrpc()
		http.Error(w, "node not found", http.StatusAccepted)
		return
	}

	for _, p := range rest.server.peerSpec {
		if p.RaftNetworkBind == n {
			_, err := rest.HandlerShutdownRemote(p.RestNetworkBind)
			if err != nil {
				http.Error(w, "node not found", http.StatusNotFound)
				return
			}
			return
		}
	}

	http.Error(w, "Key not found", http.StatusAccepted)
}

/**
REST API call to store value
*/
func (rest *Restful) SubmitKeyValue(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	rest.lock.Lock()
	defer rest.lock.Unlock()

	ctx := context.Background()

	vars := mux.Vars(r)
	key := vars["key"]
	val := vars["val"]

	if len(key) == 0 {
		http.Error(w, "empty key", http.StatusBadRequest)
		return
	}

	if len(val) == 0 {
		http.Error(w, "empty value", http.StatusBadRequest)
		return
	}

	//
	encodedKey, err := b64.URLEncoding.DecodeString(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotAcceptable)
		return
	}

	encodedVal, err := b64.URLEncoding.DecodeString(val)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotAcceptable)
		return
	}

	weblogger.WithFields(weblogger.Fields{
		"key": encodedKey,
		"val": encodedVal,
	}).Info("submit request")

	submitResp, err := rest.server.SubmitCall(ctx,
		&pb.SubmitEntry{
			Command: &pb.KeyValuePair{
				Key:   string(encodedKey),
				Value: encodedVal,
			},
		})

	if err != nil {
		glog.Errorf("failed SubmitKeyValue to a server")
		http.Error(w, err.Error(), http.StatusLocked)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	js, err := json.Marshal(submitResp)
	if err != nil {
		glog.Errorf("failed marshal json respond")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_, err = w.Write(js)
	if err != nil {
		glog.Errorf("failed write json http respond")
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func Min(x, y int) int {
	if x > y {
		return x
	}
	return y
}

/*
	REST call , return chunk of log, default chunk size 5 last record.
 	if client need indicate size, it should pass logsize in request
*/
func (rest *Restful) getLog(w http.ResponseWriter, r *http.Request) {

	log := rest.server.raftState.getLog()
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	logSize := vars["logsize"]
	limit := DefaultLogSize

	if len(logSize) > 0 {
		if i2, err := strconv.ParseInt(logSize, 10, 64); err == nil {
			limit = int(i2)
		}
	}

	var resp LogRespond
	chunkSize := Max(0, len(log)-limit)
	resp.Last_page = 0
	for i := len(log) - 1; i >= chunkSize; i-- {
		resp.Data = append(resp.Data, LogRespondEntry{
			Key:    log[i].Command.Key,
			Value:  string(log[i].Command.Value),
			Term:   log[i].Term,
			Synced: true,
		})
		resp.Last_page++
	}

	//
	js, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respSize, err := w.Write(js)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if respSize == 0 {
		http.Error(w, fmt.Errorf("empty respond").Error(), http.StatusInternalServerError)
		return
	}
}

/*
	Rest call return value based on key in request.
*/
func (rest *Restful) getValue(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	if rest.server == nil && rest.server.db == nil {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	vars := mux.Vars(r)
	key := vars["key"]

	if len(key) == 0 {
		return
	}

	encodedVal, err := b64.URLEncoding.DecodeString(key)
	if err != nil {
		http.Error(w, "failed decode key", http.StatusNotFound)
		return
	}

	submitResp, ok := rest.server.db.Get(string(encodedVal))
	if ok == false {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	// serialize and respond
	w.Header().Set("Content-Type", "application/json")
	jsRespond, err := json.Marshal(HttpValueRespond{
		Value:   submitResp,
		Success: ok,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respSize, err := w.Write(jsRespond)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if respSize == 0 {
		http.Error(w, "empty http respond", http.StatusInternalServerError)
		return
	}
}

/*
	REST call handle for shutdownGrpc server
*/
func (rest *Restful) shutdownGrpc(w http.ResponseWriter, r *http.Request) {
	rest.server.ShutdownGrpc()
}

/**
Returns all peer connection status
*/
func (rest *Restful) HandlerPeerList(w http.ResponseWriter, r *http.Request) {

	connStatus := rest.server.PeerStatus()
	js, err := json.Marshal(connStatus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	respSize, err := w.Write(js)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if respSize == 0 {
		http.Error(w, fmt.Errorf("empty http respond").Error(), http.StatusInternalServerError)
		return
	}
}

/*
	REST call, returns chunk of committed record from db,
	default chunk size 5 last records.
*/
func (rest *Restful) getCommitted(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		return
	}

	storageCopy := rest.server.db.GetCopy()
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	logSize := vars["logsize"]
	limit := DefaultLogSize

	if len(logSize) > 0 {
		if i2, err := strconv.ParseInt(logSize, 10, 64); err == nil {
			limit = int(i2)
		}
	}

	var resp []LogRespondEntry
	chunkSize := Min(len(storageCopy), limit)

	count := 0
	for k, v := range storageCopy {
		if count == chunkSize {
			break
		}
		resp = append(resp, LogRespondEntry{
			Key:   k,
			Value: string(v),
		})
		count++
	}

	//
	js, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respSize, err := w.Write(js)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if respSize == 0 {
		http.Error(w, fmt.Errorf("empty respond").Error(), http.StatusInternalServerError)
		return
	}
}

type Response struct {
	Status   int         `json:"status"`
	Meta     string      `json:"_meta"`
	Resource interface{} `json:"resource"`
}

type Reso struct {
	Output string `json:"output_data"`
}

/**

 */
func (rest *Restful) healthz(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32(&rest.healthy) == 1 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}

type TCPSet map[uint64][]flow.Hdr

type FlowData struct {
	FlowStatus map[string][]byte
}

func (f *FlowData) GetFlow(key string) []flow.Hdr {
	e := f.FlowStatus[key]
	var flow []flow.Hdr
	_ = gob.NewDecoder(bytes.NewReader(e)).Decode(&flow)
	return flow
}

type FlowRespond struct {
	// leader id
	Last_page int        `json:"last_page"`
	Data      []flow.Hdr `json:"data"`
}

/*
	REST call, returns chunk of committed record from db,
	default chunk size 5asd last records.
*/
func (rest *Restful) HandlerFlows(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		return
	}

	// TODO add option to get chunk
	storageCopy := rest.server.db.GetCopy()

	vars := mux.Vars(r)
	logSize := vars["size"]
	limit := DefaultLogSize

	weblogger.WithFields(weblogger.Fields{
		"log_size":   len(storageCopy),
		"chunk_size": logSize,
	}).Info("flow table")

	if len(logSize) > 0 {
		if i2, err := strconv.ParseInt(logSize, 10, 64); err == nil {
			limit = int(i2)
		}
	}

	chunkSize := Min(len(storageCopy), limit)
	count := 0

	flowTable := make(TCPSet)
	var flowRespond FlowRespond
	flowRespond.Last_page = chunkSize

	for k, v := range storageCopy {
		if count == chunkSize {
			break
		}

		var f []flow.Hdr
		err := gob.NewDecoder(bytes.NewReader(v)).Decode(&f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		flowRespond.Data = append(flowRespond.Data, f...)
		key, _ := strconv.Atoi(k)
		flowTable[uint64(key)] = f
		count++
	}

	//
	js, err := json.Marshal(flowRespond)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = w.Write(js)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

}

/**
A method wait on channel for signal to shutdown server.
*/
func (rest *Restful) WaitShutdown() {

	if rest == nil {
		return
	}

	irqSig := make(chan os.Signal, 1)
	signal.Notify(irqSig, syscall.SIGINT, syscall.SIGTERM)

	select {
	// interrupt handler sig term to shutdown
	case sig := <-irqSig:
		glog.Infof("Shutdown request from a signal %v", sig)
	// shutdown request
	case sig := <-rest.shutdownRequest:
		glog.Infof("Shutdown request (/shutdownGrpc %v)", sig)
	}

	atomic.StoreInt32(&rest.healthy, 0)
	glog.Infof("sending shutdown command to rest server ...")

	//Create shutdownGrpc context with 10 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := rest.restServer.Shutdown(ctx)
	if err != nil {
		glog.Infof("Shutdown request error: %v", err)
	}
}

//func logging(logger *log.Logger) func(http.Handler) http.Handler {
//	return func(next http.Handler) http.Handler {
//		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			defer func() {
//				requestID, ok := r.Context().Value(requestIDKey).(string)
//				if !ok {
//					requestID = "unknown"
//				}
//				glog.Infof(requestID, r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent())
//			}()
//			next.ServeHTTP(w, r)
//		})
//	}
//}

/**
Public handler to shutdown rest web server
*/
func (rest *Restful) ShutdownHandler(w http.ResponseWriter, r *http.Request) {

	if rest == nil {
		return
	}

	_, err := w.Write([]byte("Shutdown server"))
	if err != nil {
		glog.Errorf(err.Error())
	}
	rest.shutdownRest()
}

/*
	Shutdown a server, consumed mainly by rocinante internally
*/
func (rest *Restful) shutdownRest() {

	if rest == nil {
		return
	}

	//rest.lock.Lock()
	//defer rest.lock.Unlock()

	if !atomic.CompareAndSwapUint32(&rest.shutdownReqCount, 0, 1) {
		glog.Infof("Shutdown through API call in progress...")
		return
	}

	// write to channel and signal
	go func() {
		rest.shutdownRequest <- true
	}()
}

func (rest *Restful) StopRest() {

	if rest == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := rest.restServer.Shutdown(ctx)
	if err != nil {
		glog.Infof("Shutdown request error: %v", err)
	}
}

/*
	Index page for rest server.
*/
func (rest *Restful) HandleFlowIndex(w http.ResponseWriter, r *http.Request) {

	templateFile := filepath.Join(rest.basedir, "pkg/template/flow.html")
	tmpl := template.Must(template.ParseFiles(templateFile))

	data := PageData{ServerID: rest.server.serverBind}

	err := tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

/*
	Index page for rest server.
*/
func (rest *Restful) HandlerRole(w http.ResponseWriter, r *http.Request) {

	term, state, lastElection := rest.server.raftState.getState()
	lastUpdate := rest.server.LastUpdate

	updated := fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d 00:00\n",
		lastUpdate.Year(), lastUpdate.Month(), lastUpdate.Day(),
		lastUpdate.Hour(), lastUpdate.Minute(), lastUpdate.Second())

	election := fmt.Sprintf("%02d:%02d:%02d 00:00\n",
		lastElection.Hour(), lastElection.Minute(), lastElection.Second())

	currentServer := Info{
		ServerBind:   rest.server.serverSpec.RaftNetworkBind,
		ServerID:     strconv.FormatUint(rest.server.serverId, 10),
		Role:         state.String(),
		Term:         term,
		LastUpdate:   updated,
		LastElection: election,
	}

	//
	js, err := json.Marshal(currentServer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = w.Write(js)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

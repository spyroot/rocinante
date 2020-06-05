package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	pb "../../api"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
)

const ApiTransport string = "http://"
const ApiLeader string = "/leader"
const ApiSubmit string = "/submit"
const ApiShutdown string = "/shutdown"
const ApiLog string = "/log"
const ApiCommitted string = "/committed"
const ApiGet string = "/committed"
const ApiPeerList = "/peer/list"
const ApiIndex = "/"
const DefaultLogSize int = 5

type Restful struct {
	lock    sync.Mutex
	server  *Server
	basedir string
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

type LogRespond struct {
	// leader id
	Key    string `json:"key"`
	Value  string `json:"value"`
	Term   uint64 `json:"term"`
	Synced bool
}

/*
	Router handler for restful API
*/
func NewRestfulServer(s *Server, bind string, baseDir string) (*Restful, error) {

	r := new(Restful)
	r.server = s
	r.basedir = baseDir

	// register all end points
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc(ApiIndex, r.Index)
	router.HandleFunc("/submit/{key}/{val}", r.submit)
	router.HandleFunc(ApiCommitted, r.getCommitted)
	router.HandleFunc(ApiGet+"/{key}", r.getValue)
	router.HandleFunc(ApiLog, r.getLog)
	router.HandleFunc(ApiShutdown, r.shutdown)
	router.HandleFunc(ApiLeader, r.leader)
	router.HandleFunc(ApiPeerList, r.peerList)

	glog.Infof("[restful server started]: %s", bind)

	srv := &http.Server{
		Handler:      router,
		Addr:         bind,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	go srv.ListenAndServe()

	return r, nil
}

/*
	Index page for rest server.
*/
func (rest *Restful) Index(w http.ResponseWriter, r *http.Request) {

	templateFile := filepath.Join(rest.basedir, "pkg/template/layout.html")
	tmpl := template.Must(template.ParseFiles(templateFile))

	state := rest.server.raftState.state.String()
	term := rest.server.raftState.getTerm()
	lastUpdate := rest.server.LastUpdate
	lastElection := rest.server.raftState.electionResetEvent

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
		Role:         state,
		Term:         term,
		LastUpdate:   updated,
		LastElection: election,
	}

	data.ServerStatus = append(data.ServerStatus, currentServer)
	var prev = rest.server.serverSpec.RaftNetworkBind
	var prevID = currentServer.ServerID

	for _, p := range rest.server.peerSpec {
		currentPeerId := strconv.FormatUint(rest.server.GetPeerID(p.RaftNetworkBind), 10)

		cur := Info{
			ServerBind:   p.RaftNetworkBind,
			ServerID:     currentPeerId,
			Role:         state,
			Term:         term,
			LastUpdate:   updated,
			LastElection: election}

		cur.Connected = append(cur.Connected, Peer{ServerNei: prev, ServerID: prevID})
		data.ServerStatus = append(data.ServerStatus, cur)
		prev = p.RaftNetworkBind
		prevID = currentPeerId
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
func (rest *Restful) submit(w http.ResponseWriter, r *http.Request) {

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
		return
	}

	submitResp, err := rest.server.SubmitCall(ctx, &pb.SubmitEntry{
		Command: &pb.KeyValuePair{Key: key, Value: []byte(val)},
	})
	if err != nil {
		glog.Errorf("failed submit to a server")
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	var resp []LogRespond
	chunkSize := Max(0, len(log)-limit)
	for i := len(log) - 1; i >= chunkSize; i-- {
		resp = append(resp, LogRespond{
			Key:    log[i].Command.Key,
			Value:  string(log[i].Command.Value),
			Term:   log[i].Term,
			Synced: true,
		})
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

	submitResp, ok := rest.server.db.Get(key)
	if ok == false {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	js, err := json.Marshal(submitResp)
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
		http.Error(w, "empty http respond", http.StatusInternalServerError)
		return
	}
}

/*
	REST call handle for shutdown server
*/
func (rest *Restful) shutdown(w http.ResponseWriter, r *http.Request) {
	rest.server.Shutdown()
}

/**
Returns all peer connection status
*/
func (rest *Restful) peerList(w http.ResponseWriter, r *http.Request) {

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
	REST call, returns chunk of committed record from db, default chunk size 5 last record.
*/
func (rest *Restful) getCommitted(w http.ResponseWriter, r *http.Request) {

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

	var resp []LogRespond
	chunkSize := Max(0, len(storageCopy)-limit)

	count := 0
	for k, v := range storageCopy {
		if count == chunkSize {
			break
		}
		resp = append(resp, LogRespond{
			Key:   k,
			Value: string(v),
		})
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

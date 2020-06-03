package server

import (
	pb "../../api"
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const ApiTransport string = "http://"
const ApiLeader string = "/leader"
const ApiSubmit string = "/submit"
const ApiShutdown string = "/shutdown"
const ApiLog string = "/log"

type Restful struct {
	lock sync.Mutex
	server *Server
	basedir string
}

type ServerInfo struct {
	Title string
	ServerID  string
}


type PageData struct {
	PageTitle string
	ServerStatus  []ServerInfo
}

type LeaderRespond struct {
	// leader id
	Leader 		uint64 			`json:"leader"`
	// true if this server is leader
	Success		bool			`json:"success"`
	// grpc api client binding
	GrpcBinding string 			`json:"GrpcBinding"`
	// rest api client binding
	RestBinding string			`json:"RestBinding"`

}

//
func NewRestfulServer(s *Server, bind string, baseDir string) (*Restful, error) {

	r := new(Restful)
	r.server = s
	r.basedir = baseDir

	// register all end points
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", r.Index)
	router.HandleFunc("/submit/{key}/{val}", r.submit)
	router.HandleFunc("/get", r.getLog)
	router.HandleFunc("/get/{key}", r.getLog)
	router.HandleFunc("/log", r.getLog)
	router.HandleFunc(ApiShutdown, r.shutdown)
	router.HandleFunc(ApiLeader, r.leader)

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

//
func (rest *Restful) Index(w http.ResponseWriter, r *http.Request) {

	templateFile := filepath.Join(rest.basedir, "pkg/template/layout.html")
	tmpl := template.Must(template.ParseFiles(templateFile))

	data := PageData{
		PageTitle: "My TODO list",

		ServerStatus: []ServerInfo {
			{Title: strconv.FormatUint(rest.server.serverId, 10),
				ServerID: strconv.FormatUint(rest.server.serverId, 10)},
			{Title: "2", ServerID: "2"},
			{Title: "3", ServerID: "2"},
		},
	}

	tmpl.Execute(w, data)
}

/**

 */
func (rest *Restful) leader(w http.ResponseWriter, r *http.Request) {

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
	spec, leaderId, ok := rest.server.LastLeader()
	resp.Success = false
	resp.Leader = leaderId
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

	ctx := context.Background()

	vars := mux.Vars(r)
	key := vars["key"]
	val := vars["val"]

	if len(key) == 0 {
		return
	}

	submitResp, err := rest.server.SubmitCall(ctx, &pb.SubmitEntry {
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
		glog.Errorf("failed marshal respond")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_, err = w.Write(js)
	if err != nil {
		glog.Errorf("failed write json respond")
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

/*

 */
func (rest *Restful) getLog(w http.ResponseWriter, r *http.Request) {
	log := rest.server.raftState.getLog()
	for i, s := range log {
		fmt.Fprintln(w, i, s.Term, s.Command)
	}
}

/*

 */
func (rest *Restful) getValue(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	key := vars["key"]

	submitResp, ok := rest.server.db.Get(key)
	if ok != false {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	js, err := json.Marshal(submitResp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(js)
}

/*

 */
func (rest *Restful) shutdown(w http.ResponseWriter, r *http.Request) {
 	rest.server.Shutdown()
}



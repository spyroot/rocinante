/*
	Rocinante rest client.

	The rest client allow to store and read data from
	centralized service for maintaining key value database, configuration,
	and provide distributed synchronization and consensus.

	Rest client provides capability to send data to a cluster
	via synchronous and asynchronous primitives.

	Mustafa Bayramov
*/
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	b64 "encoding/base64"

	"../server"
	"github.com/apex/log"
	"github.com/golang/glog"
)

type RestClient struct {
	endpoint map[string]bool
	leader   string
}

const DefaultHttpTimeout time.Duration = 250

/**
Return new REST client from list of string that
host list of all rest api server end points
*/
func NewRestClient(peers []string) (*RestClient, error) {

	c := new(RestClient)
	c.endpoint = map[string]bool{}
	c.leader = ""

	if len(peers) == 0 {
		return nil, fmt.Errorf("peer list is empty")
	}

	for _, peer := range peers {
		c.endpoint[peer] = true
	}

	err := c.DiscoverLeader()
	if err != nil {
		return nil, err
	}

	return c, nil
}

/**
Return a new client , initialized from url
*/
func NewRestClientFromUrl(e string) (*RestClient, error) {

	c := new(RestClient)
	if len(e) == 0 {
		return nil, fmt.Errorf("empty peer url")
	}

	c.endpoint = map[string]bool{}
	if strings.Contains(e, "http://") {
		c.endpoint[e] = true
	} else {
		c.endpoint["http://"+e] = true

	}
	c.leader = ""

	// retrieve peer list , figure out who is leader and cache it.
	peerStatus, err := c.GetPeerList()
	if err != nil {
		return nil, fmt.Errorf("failed retrieve peer list")
	}

	for _, peers := range peerStatus {
		for _, peer := range peers {
			if strings.Contains(peer.Endpoints.RestNetworkBind, "http://") {
				c.endpoint[peer.Endpoints.RestNetworkBind] = true
			} else {
				c.endpoint["http://"+peer.Endpoints.RestNetworkBind] = true
			}
		}
	}

	_, err = c.GetLeader()
	if err != nil {
		return nil, err
	}

	return c, nil
}

/**
REST API call request a cluster leader details.
*/
func (r *RestClient) GetLeader() (*server.LeaderRespond, error) {

	var respond *server.LeaderRespond
	// timeout per request

	for peer, _ := range r.endpoint {

		apiRequest := peer + server.ApiLeader
		glog.Info("Sending request cluster req ", apiRequest)
		req, err := http.NewRequest("GET", apiRequest, nil)
		if err != nil {
			glog.Errorf("Error server unavailable %v", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), DefaultHttpTimeout*time.Millisecond)

		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			glog.Errorf("Error request timeout. Retrying next peer %v", err)
			continue
		}

		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&respond)
		if err != nil {
			glog.Infof("Failed decode respond", err)
			continue
		}

		glog.Infof("Respond %v", respond)

		if respond.Success {
			glog.Infof("Discovered cluster leader %v", respond.RestBinding, respond.Leader)
			r.leader = respond.RestBinding
			break
		}

		glog.Infof("Respond %v", respond)
		cancel()
	}

	if respond == nil {
		return nil, fmt.Errorf("all cluster member are dead")
	}

	r.leader = respond.RestBinding
	return respond, nil
}

/**
REST API call discover a leader and set
current leader details.
*/
func (r *RestClient) DiscoverLeader() error {

	if len(r.leader) == 0 {
		respond, err := r.GetLeader()
		if err != nil {
			return fmt.Errorf("cluster leader not found")
		}
		if !respond.Success {
			return fmt.Errorf("cluster leader not found")
		}

		if respond.Success {
			log.Info(respond.RestBinding)
			if !strings.Contains(server.ApiTransport, respond.RestBinding) {
				_ = fmt.Sprintf("%s%s", server.ApiTransport, respond.RestBinding)
			}
		}
	}

	return nil
}

/**
REST API call to store  key and value
*/
func (r *RestClient) Store(key string, val []byte) (bool, error) {

	err := r.DiscoverLeader()
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultHttpTimeout*time.Millisecond)
	defer cancel()

	encodedKey := b64.StdEncoding.EncodeToString([]byte(key))
	encodedVal := b64.StdEncoding.EncodeToString(val)
	apiRequest := fmt.Sprintf("http://%s%s/%s/%s", r.leader, server.ApiSubmit, encodedKey, encodedVal)

	glog.Infof("Sending request cluster req %s cluster leader %s", apiRequest, r.leader)
	req, err := http.NewRequest("GET", apiRequest, nil)
	if err != nil {
		glog.Errorf("Error server unavailable.")
		return false, nil
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		glog.Errorf("Error timeout request.")
		// try to re-discover leader
		return false, nil
	}

	if resp.StatusCode == http.StatusCreated {
		glog.Infof("Got back status %v", resp.StatusCode)
		return true, nil
	}

	return false, fmt.Errorf("failed store value")
}

/**
Returns a list of all peer and connection status.
Respond contain a map each map entry is peer spec and
grpc connection status.

Client can use this call to detect if some of the peer
are disconnected from a main cluster.
*/
func (r *RestClient) GetPeerList() (map[string]map[uint64]server.PeerStatus, error) {

	respond := make(map[string]map[uint64]server.PeerStatus)
	ctx, cancel := context.WithTimeout(context.Background(), DefaultHttpTimeout*time.Millisecond)
	defer cancel()

	for peer, _ := range r.endpoint {
		// timeout per request
		apiRequest := peer + server.ApiPeerList
		glog.Infof("Sending request to server [%v]", apiRequest)
		req, err := http.NewRequest("GET", apiRequest, nil)
		if err != nil {
			glog.Infof("Error server unavailable.")
			continue
		}

		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			glog.Infof("Error, request timeout.")
			continue
		}

		apiRespond := make(map[uint64]server.PeerStatus)
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&apiRespond)
		if err != nil {
			glog.Infof("Failed decode respond %+v", err)
			continue
		}

		glog.Infof("Respond from the server: %+v", apiRespond)
		respond[peer] = apiRespond
	}

	glog.Infof("Return %+v", respond)

	return respond, nil
}

/**
REST call to retrieve value from a server
*/
func (r *RestClient) Get(key string) (*server.HttpValueRespond, error) {

	ctx, cancel := context.WithTimeout(context.Background(), DefaultHttpTimeout*time.Millisecond)
	defer cancel()

	err := r.DiscoverLeader()
	if err != nil {
		glog.Errorf("Failed retrieve cluster leader.")
		return nil, nil
	}

	encodedKey := b64.StdEncoding.EncodeToString([]byte(key))
	apiRequest := fmt.Sprintf("http://%s%s/%s", r.leader, server.ApiGet, encodedKey)

	glog.Info("Sending request cluster req ", apiRequest)
	req, err := http.NewRequest("GET", apiRequest, nil)
	if err != nil {
		glog.Errorf("Error server unavailable %v", err)
		return nil, nil
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		glog.Errorf("Error timeout request.")
		return nil, fmt.Errorf("failed retieve value %v", err)
	}

	var respond server.HttpValueRespond
	respond.Success = false
	if resp.StatusCode == http.StatusOK {
		glog.Infof("Got back status %v", resp.StatusCode)
		// decode
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&respond)
		if err != nil {
			glog.Infof("Failed decode respond", err)
		}

		sDec, err := b64.StdEncoding.DecodeString(string(respond.Value))
		if err != nil {
			glog.Infof("Failed decode base64 respond", err)
		}

		respond.Value = sDec
		glog.Infof(" received respond %v", respond)
		return &respond, nil
	}

	glog.Infof("Got back status %v", resp.StatusCode)

	return nil, fmt.Errorf("failed store value")

}

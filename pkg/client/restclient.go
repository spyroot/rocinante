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
	endpoint []string
	leader   string
}

/**

 */
func NewRestClient(e []string) (*RestClient, error) {
	s := new(RestClient)
	s.endpoint = e
	s.leader = ""
	return s, nil
}

/**

 */
func (r *RestClient) GetLeader() (*server.LeaderRespond, error) {

	var respond *server.LeaderRespond
	for _, s := range r.endpoint {

		// timeout per request
		ctx, cancel := context.WithTimeout(context.Background(), 150 * time.Millisecond)
		defer cancel()

		apiRequest := s + server.ApiLeader
		glog.Info("Sending request cluster req ", apiRequest)
		req, err := http.NewRequest("GET", apiRequest, nil)
		if err != nil {
			glog.Infof("Error server unavailable")
			continue
		}

		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			glog.Infof("Error timeout request.")
			continue
		}

		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&respond)
		if err != nil {
			glog.Infof("Failed decode respond", err)
			continue
		}

		glog.Infof("Respond %v", respond)
	}

	if respond == nil {
		glog.Infof("All cluster member are dead.")
	}

	r.leader =  respond.RestBinding
	return respond, nil
}

/**

 */
func (r *RestClient) Store(key string, val []byte) (bool, error) {

	if len(r.leader) == 0 {
		respond, err := r.GetLeader()
		if err != nil {
			return false, fmt.Errorf("cluster leader not found")
		}
		if !respond.Success {
			return false, fmt.Errorf("cluster leader not found")
		}

		log.Info(respond.RestBinding)
		if !strings.Contains(server.ApiTransport, respond.RestBinding) {
			fmt.Sprintf("%s%s", server.ApiTransport, respond.RestBinding)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150 * time.Millisecond)
	defer cancel()

	encodedKey := b64.StdEncoding.EncodeToString(val)
	encodedVal := b64.StdEncoding.EncodeToString(val)
	apiRequest := fmt.Sprintf("http://%s%s/%s/%s", r.leader, server.ApiSubmit, encodedKey, encodedVal)

	glog.Info("Sending request cluster req ", apiRequest)
	req, err := http.NewRequest("GET", apiRequest, nil)
	if err != nil {
		glog.Errorf("Error server unavailable.")
		return false, nil
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		glog.Errorf("Error timeout request.")
		return false, nil
	}

	if resp.StatusCode == http.StatusCreated {
		return true, nil
	}

	return false, fmt.Errorf("failed store value")
}



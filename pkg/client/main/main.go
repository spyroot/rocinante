package main

import (
	"math/rand"
	"time"

	. "../../client"
	"github.com/apex/log"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

/*
  Example rest client API
*/
func main() {

	rand.Seed(time.Now().UnixNano())

	// Set up a connection to the server.
	apiClient, err := NewRestClientFromUrl("http://localhost:8001")
	if err != nil {
		log.Fatal(err.Error())
		return
	}

	for {
		key := randSeq(12)
		val := []byte(randSeq(12))

		ok, err := apiClient.Store(key, val)
		if err == nil {
			if ok == true {
				resp, httpErr := apiClient.Get(key)
				if httpErr != nil {
					log.Infof("Got respond back %v", resp)
				}
			}
		} else {
			log.Infof("Failed", err)
		}

		time.Sleep(5 * time.Second)
	}

}

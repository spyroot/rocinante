package loadbalancer

import (
	"net/http/httputil"
	"net/url"
	"sync"
)

// Backend holds the data about a server
type ServerFarm struct {
	URL          *url.URL
	mux          sync.RWMutex
	Alive        bool
	ReverseProxy *httputil.ReverseProxy
}

// SetAlive for this backend
func (b *ServerFarm) SetAlive(alive bool) {
	b.mux.Lock()
	b.Alive = alive
	b.mux.Unlock()
}

// IsAlive returns true when backend is alive
func (b *ServerFarm) IsAlive() (alive bool) {
	b.mux.RLock()
	alive = b.Alive
	b.mux.RUnlock()
	return
}

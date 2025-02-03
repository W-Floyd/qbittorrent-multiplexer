package state

import (
	"net/http"
	"net/http/httputil"
	"sync"
)

var (
	AppState = State{
		NumberOfClients: 2,
		BalancerCount:   0,
		Proxies:         map[uint]*httputil.ReverseProxy{},
		Torrents:        map[string]uint{},
		Cookies:         map[uint]http.Cookie{},
	}
)

/// TODO: Create cookie store so we can augment proxy requests

type State struct {
	NumberOfClients uint
	BalancerCount   uint `json:"-"`

	Proxies  map[uint]*httputil.ReverseProxy `json:"-"`
	Torrents map[string]uint                 `json:"-"`
	Cookies  map[uint]http.Cookie            `json:"-"`

	Locks struct {
		Cookies  sync.Mutex `json:"-"`
		Proxies  sync.Mutex `json:"-"`
		Torrents sync.Mutex `json:"-"`
	} `json:"-"`
}

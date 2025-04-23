package orv

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/rs/zerolog"
)

const (
	_API_NAME    string = "Orv"
	_API_VERSION string = "0.0.1"
)

/*
A single instance of a Vault Keeper.
Able to provide services, route & answer requests, and facilitate vault growth.
*/
type VaultKeeper struct {
	log  zerolog.Logger // output logger
	id   uint64         // unique identifier
	addr netip.AddrPort
	// services
	children map[string]struct {
		id       uint64
		services map[string]string // service name -> endpoint
	}
	endpoint struct {
		api huma.API
		mux *http.ServeMux
	}
}

type helloOut struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

// Spawns and returns a new vault keeper instance.
func NewVaultKeeper(id uint64, logger zerolog.Logger, addr netip.AddrPort) *VaultKeeper {
	// teach the muxer
	mux := http.NewServeMux()
	vk := &VaultKeeper{
		log:  logger,
		id:   id,
		addr: addr,
		children: map[string]struct {
			id       uint64
			services map[string]string
		}{},
		endpoint: struct {
			api huma.API
			mux *http.ServeMux
		}{
			api: humago.New(mux, huma.DefaultConfig(_API_NAME, _API_VERSION)),
			mux: mux,
		},
	}

	huma.Get(vk.endpoint.api, HELLO, func(ctx context.Context, input *struct{}) (*helloOut, error) {
		resp := &helloOut{}
		resp.Body.Message = "Hello, Orv!"
		return resp, nil
	})

	return vk
}

func (vk *VaultKeeper) Start() error {
	return http.ListenAndServe(vk.addr.String(), vk.endpoint.mux)
}

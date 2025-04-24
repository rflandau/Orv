package orv

import (
	"net/http"
	"net/netip"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/rs/zerolog"
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
	heightRWMu sync.RWMutex // locker for height
	height     uint16       // current height of this vk
}

// Spawns and returns a new vault keeper instance.
func NewVaultKeeper(id uint64, logger zerolog.Logger, addr netip.AddrPort) *VaultKeeper {
	mux := http.NewServeMux()

	// validate the given address
	if !addr.IsValid() {
		// TODO return error
	}

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
		height: 0, // TODO take in a parameter if the developer wishes to utilize dragon's hoard
	}

	vk.buildRoutes()

	// TODO spawn a goro to prune the state maps (ex: the hello map and child services map)

	return vk
}

// Starts the http api listener in the vk.
// Currently blocking. // TODO
func (vk *VaultKeeper) Start() error {
	vk.log.Info().Str("address", vk.addr.String()).Msg("listening...")

	// TODO convert this into a real http.Server so we can call .Shutdown on termination
	return http.ListenAndServe(vk.addr.String(), vk.endpoint.mux)
}

// Stops the http api listener.
// Currently ineffectual until we switch to a real http.Server
func (vk *VaultKeeper) Stop() {
	// TODO
	// TODO include graceful shutdown: https://huma.rocks/how-to/graceful-shutdown/

}

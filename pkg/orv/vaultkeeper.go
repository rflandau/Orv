package orv

import (
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/rs/zerolog"
)

// Default durations after the K/V may be pruned
// (it is not guaranteed to be pruned at exactly this time, but its survival cannot guarantee after this point)
const (
	DEFAULT_PRUNE_TIME_PENDING_HELLO time.Duration = time.Second * 20
)

//#region types

// The amount of time before values in each table are prune-able.
type PruneTimes struct {
	pendingHello time.Duration
}

/*
A single instance of a Vault Keeper.
Able to provide services, route & answer requests, and facilitate vault growth.
Should be constructed via NewVaultKeeper().
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

	pt PruneTimes

	pendingHellos sync.Map // id -> timestamp
}

// Function to set various options on the vault keeper.
// Uses defaults if an option is not set.
type VKOption func(*VaultKeeper)

//#endregion types

//#region options

// Set the prune times of VaultKeeper to the values stored in pt
func SetPruneTimes(pt PruneTimes) VKOption {
	return func(vk *VaultKeeper) {
		vk.pt = pt
	}
}

// Set the starting height of the VK (giving it a "hoard").
func Height(h uint16) VKOption {
	return func(vk *VaultKeeper) {
		vk.height = h
	}
}

//#endregion options

// Spawns and returns a new vault keeper instance.
//
// Optionally takes additional options to modify the state of the VaultKeeper.
// Conflicting options prefer options latter.
func NewVaultKeeper(id uint64, logger zerolog.Logger, addr netip.AddrPort, opts ...VKOption) *VaultKeeper {
	mux := http.NewServeMux()

	// validate the given address
	if !addr.IsValid() {
		// TODO return error
	}

	// set defaults
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

		pt: PruneTimes{pendingHello: DEFAULT_PRUNE_TIME_PENDING_HELLO},
	}

	vk.buildRoutes()

	// apply the given options
	for _, opt := range opts {
		opt(vk)
	}

	// TODO spawn a goro to prune the state maps (ex: the hello map and child services map)

	// dump out data about the vault keeper
	vk.log.Debug().Func(func(e *zerolog.Event) {
		e.Str("state", vk.Dump())
	}).Msg("New Vault Keeper created")

	return vk
}

//#region methods

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

// Returns a pretty string of the current state of the vault keeper.
// Used for debugging purposes.
func (vk *VaultKeeper) Dump() string {
	var sb strings.Builder
	//TODO

	return sb.String()
}

//#endregion methods

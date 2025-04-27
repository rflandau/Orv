package orv

import (
	"fmt"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/rs/zerolog"
)

// type aliases for readability
type (
	childID = uint64
)

// Default durations after the K/V may be pruned
// (it is not guaranteed to be pruned at exactly this time, but its survival cannot guarantee after this point)
const (
	DEFAULT_PRUNE_TIME_PENDING_HELLO     time.Duration = time.Second * 20
	DEFAULT_PRUNE_TIME_SERVICELESS_CHILD time.Duration = time.Second * 20
)

//#region types

// The amount of time before values in each table are prune-able.
type PruneTimes struct {
	pendingHello time.Duration
	// after a child joins, how long do they have to register a service before getting pruned?
	servicelessChild time.Duration
}

type srv struct {
	address   netip.AddrPort
	staleness time.Duration
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
	childrenMu sync.Mutex                 // locker for leaves+childVKs
	leaves     map[childID]map[string]srv // cID -> (service name -> address&staleness)
	childVKs   map[childID]struct {
		lastHeartbeat time.Time
		services      map[string]bool //service name --> 1
	}

	endpoint struct {
		api  huma.API
		mux  *http.ServeMux
		http http.Server
	}

	structureRWMu sync.RWMutex // locker for height+parent
	height        uint16       // current height of this vk
	parent        struct {
		id   uint64 // 0 if we are root
		addr netip.AddrPort
	}
	pt PruneTimes

	// TODO potentially convert to id -> time.Timer and have the pruner select on the timer channels
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
func NewVaultKeeper(id uint64, logger zerolog.Logger, addr netip.AddrPort, opts ...VKOption) (*VaultKeeper, error) {
	mux := http.NewServeMux()

	// validate the given address
	if !addr.IsValid() {
		return nil, ErrBadAddr(addr)
	}

	// set defaults
	vk := &VaultKeeper{
		log:    logger,
		id:     id,
		addr:   addr,
		leaves: map[childID]map[string]srv{},
		childVKs: map[childID]struct {
			lastHeartbeat time.Time
			services      map[string]bool //service name --> 1
		}{},
		endpoint: struct {
			api  huma.API
			mux  *http.ServeMux
			http http.Server // TODO populate with new server
		}{
			api: humago.New(mux, huma.DefaultConfig(_API_NAME, _API_VERSION)),
			mux: mux,
		},
		height: 0,

		pt: PruneTimes{
			pendingHello:     DEFAULT_PRUNE_TIME_PENDING_HELLO,
			servicelessChild: DEFAULT_PRUNE_TIME_SERVICELESS_CHILD,
		},
	}

	vk.buildEndpoints()

	// apply the given options
	for _, opt := range opts {
		opt(vk)
	}

	// TODO spawn a goro to prune the state maps (ex: the hello map and child services map)
	go func() {
		for {
			/*select {
				// some cancellable channel
			}*/
			vk.pendingHellos.Range(func(key, value any) bool {
				// cast value to time.Time
				// TODO
				return false
			})

			// TODO check that each child has at least one service associated to it
			// if it does not, check if its join-grace period has elapsed
			// if it has, prune it
		}
	}()

	// dump out data about the vault keeper
	vk.log.Debug().Func(vk.LogDump).Msg("New Vault Keeper created")

	return vk, nil
}

//#region methods

// Starts the http api listener in the vk.
// Currently blocking. // TODO
func (vk *VaultKeeper) Start() error {
	vk.log.Info().Str("address", vk.addr.String()).Msg("listening...")

	// TODO convert this into a real http.Server so we can call .Shutdown on termination
	// return http.ListenAndServe(vk.addr.String(), vk.endpoint.mux)

	// Create the HTTP server.
	vk.endpoint.http = http.Server{
		Addr:    vk.addr.String(),
		Handler: vk.endpoint.mux,
	}
	return vk.endpoint.http.ListenAndServe()
}

// Stops the http api listener.
// Currently ineffectual until we switch to a real http.Server
func (vk *VaultKeeper) Stop() {
	// TODO
	// TODO include graceful shutdown: https://huma.rocks/how-to/graceful-shutdown/
	vk.endpoint.http.Close()
}

func (vk *VaultKeeper) isRoot() bool {
	return vk.parent.id != 0
}

// Used to register a new service that this VK offers locally.
func (vk *VaultKeeper) RegisterLocalService() {
	// TODO
}

// Pretty prints the state of the vk into the given zerolog event.
// Used for debugging purposes.
func (vk *VaultKeeper) LogDump(e *zerolog.Event) {
	// e.Uint64(vk.id) // assumed to exist
	e.Uint16("height", vk.height)
	// iterate through your children
	for cid, services := range vk.leaves {
		m := zerolog.Dict()
		for sn, srv := range services {
			m.Any(sn, srv)
		}

		e.Dict(fmt.Sprintf("child %d", cid), m)
	}
}

//#endregion methods

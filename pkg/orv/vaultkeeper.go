package orv

import (
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/rs/zerolog"
)

// type aliases for readability
type (
	childID     = uint64 // id of a child (may be a leaf or a cVK)
	serviceName = string
)

// Default durations after the K/V may be pruned
// (it is not guaranteed to be pruned at exactly this time, but its survival cannot guarantee after this point)
const (
	DEFAULT_PRUNE_TIME_PENDING_HELLO     time.Duration = time.Second * 5
	DEFAULT_PRUNE_TIME_SERVICELESS_CHILD time.Duration = time.Second * 20
	DEFAULT_PRUNE_TIME_CVK               time.Duration = time.Second * 20
)

//#region types

// The amount of time before values in each table are prune-able.
type PruneTimes struct {
	PendingHello time.Duration
	// after a child joins, how long do they have to register a service before getting pruned?
	ServicelessChild time.Duration
	// how long can a child CVK survive without sending a heartbeat
	CVK time.Duration
}

/*
A single instance of a Vault Keeper.
Able to provide services, route & answer requests, and facilitate vault growth.
Should be constructed via NewVaultKeeper().
*/
type VaultKeeper struct {
	log  *zerolog.Logger // output logger
	id   uint64          // unique identifier
	addr netip.AddrPort
	// services
	children *children

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
	pt      PruneTimes
	pchDone chan bool // used to notify the pruner goro that it is time to shut down

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

// Override the default, verbose logger.
// To disable logging, pass a disabled zerolog logger.
func SetLogger(l *zerolog.Logger) VKOption {
	if l == nil {
		panic("cannot set logger to nil")
	}
	return func(vk *VaultKeeper) {
		vk.log = l
	}
}

// Override the default huma API instance.
// NOTE(_): This option is applied before routes are built, meaning routes will be built onto it, potentially destructively.
func SetHumaAPI(api huma.API) VKOption {
	return func(vk *VaultKeeper) {
		vk.endpoint.api = api
	}
}

//#endregion options

// Spawns and returns a new vault keeper instance.
//
// Optionally takes additional options to modify the state of the VaultKeeper.
// Conflicting options prefer options latter.
func NewVaultKeeper(id uint64, addr netip.AddrPort, opts ...VKOption) (*VaultKeeper, error) {

	// validate the given address
	if !addr.IsValid() {
		return nil, ErrBadAddr(addr)
	}

	// set defaults
	vk := &VaultKeeper{
		id:   id,
		addr: addr,

		endpoint: struct {
			api  huma.API
			mux  *http.ServeMux
			http http.Server // TODO populate with new server
		}{
			mux: http.NewServeMux(),
		},
		height: 0,

		pt: PruneTimes{
			PendingHello:     DEFAULT_PRUNE_TIME_PENDING_HELLO,
			ServicelessChild: DEFAULT_PRUNE_TIME_SERVICELESS_CHILD,
			CVK:              DEFAULT_PRUNE_TIME_CVK,
		},
		pchDone: make(chan bool),
	}

	// apply the given options
	for _, opt := range opts {
		opt(vk)
	}
	// if the api handler was not set by the options, use the default handler
	if vk.endpoint.api == nil {
		vk.endpoint.api = humago.New(vk.endpoint.mux, huma.DefaultConfig(_API_NAME, _API_VERSION))
	}

	vk.buildEndpoints()

	// if logger was not set by the options, use the default logger
	if vk.log == nil {
		l := zerolog.New(zerolog.ConsoleWriter{
			Out:         os.Stdout,
			FieldsOrder: []string{"vkid"},
			TimeFormat:  "15:04:05",
		}).With().
			Uint64("vk", vk.id).
			Timestamp().
			Caller().
			Logger().Level(zerolog.DebugLevel)
		vk.log = &l
	}

	// generate child handling
	vk.children = newChildren(vk.log, vk.pt.ServicelessChild, vk.pt.CVK)

	// spawn a pruner service to clean up pieces of VK that are not self-pruning
	go func() {
		// generate a sublogger for the pruner to use
		l := vk.log.With().Str("sublogger", "pruner").Logger()
		helloPruneDur := vk.pt.PendingHello

		l.Debug().Msg("pruner online")
		for {
			select {
			case <-vk.pchDone:
				l.Debug().Msg("pruner shutting down...")
				return
			default:
				// check if any HELLOs need to be pruned
				vk.pendingHellos.Range(func(key, value any) bool {
					// cast key to id
					id, ok := key.(childID)
					if !ok {
						l.Error().Any("raw key", key).Any("raw value", value).Msg("failed to type assert key as uint64")
						return true
					}
					// cast value to time.Time
					ts, ok := value.(time.Time)
					if !ok {
						l.Error().Uint64("id", id).Any("raw value", value).Msg("failed to type assert value as timestamp")
						return true
					}
					if time.Since(ts) >= helloPruneDur {
						vk.pendingHellos.Delete(id)
						l.Debug().Uint64("id", id).Msg("pruned hello")
					}
					return true // also continue, until we have visited every key
				})
			}
		}
	}()

	// dump out data about the vault keeper
	vk.log.Debug().Func(vk.LogDump).Msg("New Vault Keeper created")

	return vk, nil
}

//#region methods

// Starts the http api listener in the vk.
func (vk *VaultKeeper) Start() error {
	vk.log.Info().Str("address", vk.addr.String()).Msg("listening...")

	// Create the HTTP server.
	vk.endpoint.http = http.Server{
		Addr:    vk.addr.String(),
		Handler: vk.endpoint.mux,
	}
	go vk.endpoint.http.ListenAndServe()
	return nil
}

// Terminates the vaultkeeper, cleaning up all resources and closing the API server.
func (vk *VaultKeeper) Terminate() {
	// kill the pruner
	close(vk.pchDone)
	// TODO clean up resources
	err := vk.endpoint.http.Close()
	vk.log.Info().Str("address", vk.addr.String()).AnErr("close error", err).Msg("killed http server")
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
	e.Uint16("height", vk.height)
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()
	// iterate through your children
	for cid, srvMap := range vk.children.leaves { // leaves
		a := zerolog.Arr()
		for sn, _ := range srvMap {
			a.Str(sn)
		}

		e.Array(fmt.Sprintf("leaf %d", cid), a)
	}

	for cid, v := range vk.children.vks { // child VKs
		a := zerolog.Arr()
		for sn, _ := range v.services {
			a.Str(sn)
		}

		e.Array(fmt.Sprintf("cVK %d", cid), a)
	}
}

//#endregion methods

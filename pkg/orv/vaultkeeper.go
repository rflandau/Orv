package orv

/*
The brain and body of the prototype, this file defines the VaultKeeper class, options for configuring it, and the defaults it uses for timers.
*/

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
	"resty.dev/v3"
)

// type aliases for readability
type (
	childID     = uint64 // id of a child (may be a leaf or a cVK)
	serviceName = string
)

// Default durations for the prune timers to fire.
// Records are not guaranteed be pruned at exactly this time, but its survival cannot be guaranteed after this point.
const (
	DEFAULT_PRUNE_TIME_PENDING_HELLO     time.Duration = time.Second * 5
	DEFAULT_PRUNE_TIME_SERVICELESS_CHILD time.Duration = time.Second * 5
	DEFAULT_PRUNE_TIME_CVK               time.Duration = time.Second * 5
)

// Default frequency at which a VK sends heartbeats to its parent.
const DEFAULT_PARENT_HEARTBEAT_FREQ time.Duration = 500 * time.Millisecond

//#region types

// The amount of time before values in each table are prune-able.
type PruneTimes struct {
	// after receiving a HELLO, how long until the HELLO is forgotten?
	PendingHello time.Duration
	// after a child joins, how long do they have to register a service before getting pruned?
	ServicelessChild time.Duration
	// how long can a child CVK survive without sending a heartbeat?
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

	restClient *resty.Client // client for hitting the endpoints of other VKs

	structureRWMu sync.RWMutex // locker for height+parent
	height        uint16       // current height of this vk
	parent        struct {
		id   uint64 // 0 if we are root
		addr netip.AddrPort
	}
	parentHeartbeatFrequency time.Duration // how often do we heartbeat our parent?
	pt                       PruneTimes
	helperDoneCh             chan bool // used to notify the pruner and heartbeater goros that it is time to shut down

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
			http http.Server
		}{
			mux: http.NewServeMux(),
		},
		height: 0,

		restClient: resty.New(),

		parentHeartbeatFrequency: DEFAULT_PARENT_HEARTBEAT_FREQ,
		pt: PruneTimes{
			PendingHello:     DEFAULT_PRUNE_TIME_PENDING_HELLO,
			ServicelessChild: DEFAULT_PRUNE_TIME_SERVICELESS_CHILD,
			CVK:              DEFAULT_PRUNE_TIME_CVK,
		},
		helperDoneCh: make(chan bool),
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
			case <-vk.helperDoneCh:
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

	// spawn a service to send heartbeats to the parent of this VK (if applicable)
	go vk.startHeartbeater()

	// dump out data about the vault keeper
	vk.log.Debug().Func(vk.LogDump).Msg("New Vault Keeper created")

	return vk, nil
}

// Infinite call to continually send heartbeats to the parent, if one is set on the vk.
//
// Intended to be run in a new goroutine.
//
// Dies only when vk.helperDoneCh is closed.
func (vk *VaultKeeper) startHeartbeater() {
	l := vk.log.With().Str("sublogger", "heartbeater").Logger()
	smpl := l.Sample(&zerolog.Sometimes).With().Str("sampled", "sometimes").Logger() // create a sampled logger for HB messages
	freq := vk.parentHeartbeatFrequency

	for {
		select {
		case <-vk.helperDoneCh:
			l.Debug().Msg("heartbeater shutting down...")
			return
		case <-time.After(freq):
			vk.structureRWMu.RLock()
			if !vk.isRoot() {
				smpl.Debug().Msg("sending HB to parent...")

				parentUrl := "http://" + vk.parent.addr.String() + EP_VK_HEARTBEAT
				// send a heartbeat to the parent
				hbResp := &VKHeartbeatAck{}

				res, err := vk.restClient.R().
					SetBody(VKHeartbeatReq{Body: struct {
						Id uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier of the child VK being refreshed\""
					}{vk.id}}.Body). // default request content type is JSON
					SetExpectResponseContentType(CONTENT_TYPE).
					SetResult(hbResp). // or SetResult(LoginResponse{}).
					Post(parentUrl)
				if err != nil {
					l.Warn().Err(err).Msg("failed to heartbeat parent")
					// throw away parent
					vk.parent.id = 0
					vk.parent.addr = netip.AddrPort{}
				} else if res.StatusCode() != EXPECTED_STATUS_VK_HEARTBEAT {
					l.Warn().Int("status", res.StatusCode()).Msg("bad response code when heartbeating parent")
					// throw away parent
					vk.parent.id = 0
					vk.parent.addr = netip.AddrPort{}
				} else { // success
					smpl.Debug().Uint64("parent id", vk.parent.id).Msg("successfully heartbeated parent")
				}
			}
			vk.structureRWMu.RUnlock()
		}
	}
}

//#region getters

// Return's ID of the VK.
func (vk *VaultKeeper) ID() childID {
	return vk.id
}

// Return's addr of the VK.
func (vk *VaultKeeper) AddrPort() netip.AddrPort {
	return vk.addr
}

// Return's height of the VK.
func (vk *VaultKeeper) Height() uint16 {
	vk.structureRWMu.RLock()
	defer vk.structureRWMu.RUnlock()
	return vk.height
}

// Returns the name of each service offered and by whom.
func (vk *VaultKeeper) ChildrenSnapshot() ChildrenSnapshot {
	return vk.children.Snapshot()
}

// Returns the parent of the VK. 0 and netip.AddrPort{} if root.
func (vk *VaultKeeper) Parent() struct {
	Id   uint64
	Addr netip.AddrPort
} {
	vk.structureRWMu.RLock()
	defer vk.structureRWMu.RUnlock()
	return struct {
		Id   uint64
		Addr netip.AddrPort
	}{vk.parent.id, vk.parent.addr}
}

//#region methods

// Starts the http api listener in the vk.
// Includes a small start up delay to ensure the server is ready by the time this function returns.
func (vk *VaultKeeper) Start() error {
	vk.log.Info().Str("address", vk.addr.String()).Msg("listening...")

	// Create the HTTP server.
	vk.endpoint.http = http.Server{
		Addr:    vk.addr.String(),
		Handler: vk.endpoint.mux,
	}
	go vk.endpoint.http.ListenAndServe()
	time.Sleep(600 * time.Millisecond) // give the server time to start up before returning
	return nil
}

// Terminates the vaultkeeper, cleaning up all resources and closing the API server.
func (vk *VaultKeeper) Terminate() {
	// kill the pruner and heartbeater
	close(vk.helperDoneCh)
	// kill resty
	vk.restClient.Close()

	err := vk.endpoint.http.Close()
	vk.log.Info().Str("address", vk.addr.String()).AnErr("close error", err).Msg("killed http server")
}

// Returns whether or not we believe we are the root of the vault.
// Caller is expected to hold the structureLock, lest we create a data race.
func (vk *VaultKeeper) isRoot() bool {
	return vk.parent.id == 0
}

// Causes the VaultKeeper to send a HELLO to the given address
func (vk *VaultKeeper) Hello(addrStr string) (resp *resty.Response, err error) {
	addr, err := netip.ParseAddrPort(addrStr)
	if err != nil {
		return nil, err
	}

	return vk.restClient.R().SetBody(HelloReq{Body: struct {
		Id uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
	}{vk.id}}.Body).Post("http://" + addr.String() + EP_HELLO)
}

// Causes the VaultKeeper to attempt to join the VK at the given address.
// If it succeeds, the VK will alters its current parent to point to the new parent.
// Expects that the caller already sent HELLO.
func (vk *VaultKeeper) Join(addrStr string) (err error) {
	// validate the parent address
	addr, err := netip.ParseAddrPort(addrStr)
	if err != nil {
		return err
	}

	parentURL := "http://" + addr.String() + EP_JOIN
	vk.log.Info().Str("target VK", parentURL).Msg("requesting to join VK as child")
	vk.structureRWMu.Lock()
	defer vk.structureRWMu.Unlock()

	var joinResp JoinAcceptResp

	res, err := vk.restClient.R().
		SetBody(JoinReq{Body: struct {
			Id     uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
			Height uint16 "json:\"height,omitempty\" dependentRequired:\"is-vk\" example:\"3\" doc:\"height of the vk attempting to join the vault\""
			VKAddr string "json:\"vk-addr,omitempty\" dependentRequired:\"is-vk\" example:\"174.1.3.4:8080\" doc:\"address of the listening VK service that can receive INCRs\""
			IsVK   bool   "json:\"is-vk,omitempty\" example:\"false\" doc:\"is this node a VaultKeeper or a leaf? If true, height and VKAddr are required\""
		}{Id: vk.id, Height: vk.height, VKAddr: vk.addr.String(), IsVK: true}}.Body). // default request content type is JSON
		SetExpectResponseContentType(CONTENT_TYPE).
		SetResult(&(joinResp.Body)).
		Post(parentURL)
	if err != nil {
		vk.log.Warn().Err(err).Any("response", joinResp).Msg("failed to join under VK")
		return fmt.Errorf("failed to join under VK: %v (response: %v)", err, joinResp)
	} else if res.StatusCode() != EXPECTED_STATUS_JOIN {
		vk.log.Warn().Int("status", res.StatusCode()).Msg("")
		return fmt.Errorf("bad response code when joining under VK: %d (response: %v)", res.StatusCode(), res.String())
	} else { // success
		vk.log.Debug().Uint64("parent id", joinResp.Body.Id).Msg("successfully joined under VK")
		// update parent information
		vk.parent.id = joinResp.Body.Id
		vk.parent.addr = addr
	}
	return nil
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
		for sn := range srvMap {
			a.Str(sn)
		}

		e.Array(fmt.Sprintf("leaf %d", cid), a)
	}

	for cid, v := range vk.children.vks { // child VKs
		a := zerolog.Arr()
		for sn := range v.services {
			a.Str(sn)
		}

		e.Array(fmt.Sprintf("cVK %d", cid), a)
	}
}

//#endregion methods

// Package vaultkeeper implements a vaultkeeper, the primary node of a vault.
// A vaultkeeper can be spun up with New and directed using the subroutines in requests.go.
package vaultkeeper

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"github.com/rflandau/Orv/implementations/slims/slims/vk/expiring"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

//#region defaults

const (
	DefaultHelloPruneTime            time.Duration = 3 * time.Second
	DefaultHeartbeatlessCVKPruneTime time.Duration = 3 * time.Second
	DefaultServicelessLeafPruneTime  time.Duration = 3 * time.Second
	DefaultParentHBFreq              time.Duration = 1 * time.Second

	DefaultBadHeartbeatLimit uint = 3
)

var (
	defaultVersionsSupported = version.NewSet(version.Version{Major: 1, Minor: 0})
)

//#endregion defaults

// A VaultKeeper (VK) is a node with organizational and routing capability.
// The key building block of a Vault.
type VaultKeeper struct {
	log  *zerolog.Logger
	id   slims.NodeID   // unique identifier of this node
	addr netip.AddrPort // address this vk accepts packets on
	net  struct {
		accepting atomic.Bool        // are we currently accepting connections?
		pconn     net.PacketConn     // the packet-oriented UDP connection we are listening on
		ctx       context.Context    // the context pconn is running under
		cancel    context.CancelFunc // callable to kill ctx
	}

	versionSet version.Set

	// vault information relevant to us
	structure struct {
		mu         sync.RWMutex // must be held to interact with this struct
		height     uint16       // current node height
		parentID   slims.NodeID // 0 if we are root
		parentAddr netip.AddrPort
	}

	// how quickly are pieces of data pruned
	pruneTime struct {
		hello           time.Duration // hello without join
		servicelessLeaf time.Duration // time after join, if no services are registered
		cvk             time.Duration // w/o VK_HEARTBEAT
		//leaf time.Duration // w/o SERVICE_
	}

	// direct children of this node.
	// NOTE(rlandau): leaves are mildly too complex to be represented by a single expiring table.
	children struct {
		mu sync.Mutex // must be held to interact with this struct

		// child vks
		cvks expiring.Table[slims.NodeID, struct {
			services map[string]netip.AddrPort // service name -> service address
			addr     netip.AddrPort            // address that the vk service is remotely accessible at
		}]
		// non-routing/serving children
		leaves map[slims.NodeID]leaf
		// alternate view of the cvks and leaves fields.
		// service name -> (ID of child providing the service -> address of the service
		allServices map[string]map[slims.NodeID]netip.AddrPort
	}

	// Hellos we have received but that have not yet been followed by a JOIN
	pendingHellos expiring.Table[slims.NodeID, bool]
	// hearbeat handling
	// NOTE(rlandau): uses net.accepting to determine whether or not to send heartbeats
	hb struct {
		// do we send heartbeats automatically?
		// Defaults to true.
		auto bool
		// how often do we send heartbeats
		freq time.Duration
		// when we fail to heartbeat our parent this many times consecutively, the parent will be dropped
		badHeartbeatLimit uint
	}
}

// New generates a new VK instance, optionally modified with opts.
// The returned VK is ready for use as soon as it is .Start()'d.
func New(id uint64, addr netip.AddrPort, opts ...VKOption) (*VaultKeeper, error) {
	if !addr.IsValid() {
		return nil, ErrBadAddr(addr)
	}

	// set defaults
	vk := &VaultKeeper{
		//alive: &atomic.Bool{},
		id:   id,
		addr: addr,
		net: struct {
			accepting atomic.Bool
			pconn     net.PacketConn
			ctx       context.Context
			cancel    context.CancelFunc
		}{},

		versionSet: defaultVersionsSupported,

		structure: struct {
			mu         sync.RWMutex
			height     uint16
			parentID   uint64
			parentAddr netip.AddrPort
		}{},
		pruneTime: struct {
			hello           time.Duration
			servicelessLeaf time.Duration
			cvk             time.Duration
		}{
			hello:           DefaultHelloPruneTime,
			servicelessLeaf: DefaultServicelessLeafPruneTime,
			cvk:             DefaultHeartbeatlessCVKPruneTime,
		},
		children: struct {
			mu   sync.Mutex
			cvks expiring.Table[slims.NodeID, struct {
				services map[string]netip.AddrPort
				addr     netip.AddrPort
			}]
			leaves      map[slims.NodeID]leaf
			allServices map[string]map[slims.NodeID]netip.AddrPort
		}{
			cvks: expiring.Table[slims.NodeID, struct {
				services map[string]netip.AddrPort
				addr     netip.AddrPort
			}]{},
			leaves:      make(map[slims.NodeID]leaf),
			allServices: make(map[string]map[slims.NodeID]netip.AddrPort),
		},
		hb: struct {
			auto              bool
			freq              time.Duration
			badHeartbeatLimit uint
		}{true, DefaultParentHBFreq, DefaultBadHeartbeatLimit},
	}
	vk.net.accepting.Store(false)

	// apply options
	for _, opt := range opts {
		opt(vk)
	}

	// if the logger was not established by the options, generate the default logger
	if vk.log == nil {
		l := zerolog.New(zerolog.ConsoleWriter{
			Out:         os.Stdout,
			FieldsOrder: []string{"vkid"},
			TimeFormat:  "15:04:05",
		}).With().
			Uint64("vkid", vk.id).
			Timestamp().
			Caller().
			Logger().Level(zerolog.DebugLevel)
		vk.log = &l
	}

	vk.log.Debug().Func(vk.Zerolog).Msg("vk created")

	// spin out the heartbeater
	go vk.runAutoHeartbeat()

	return vk, nil

}

// runAutoHeartbeat controls the automated heartbeats sent to parent every interval
// (but only if we are accepting connections at interval time).
//
// Intended to be run in a goroutine.
func (vk *VaultKeeper) runAutoHeartbeat() {
	if vk.hb.auto {
		// spool up a couple new loggers to use
		hbLog := vk.log.With().Str("sublogger", "autoHB").Logger()
		hbSampled := hbLog.Sample(zerolog.Sometimes)

		hbLog.Debug().Dur("frequency", vk.hb.freq).Uint("limit", vk.hb.badHeartbeatLimit).Msg("starting auto heartbeater")
		tkr := time.NewTicker(vk.hb.freq)
		badHBCount := 0
		for {
			<-tkr.C
			hbSampled.Debug().Msg("heartbeater waking")
			if !vk.net.accepting.Load() {
				continue
			}
			// cache parent information
			vk.structure.mu.RLock()
			p, pAddr := vk.structure.parentID, vk.structure.parentAddr
			vk.structure.mu.RUnlock()

			if !pAddr.IsValid() {
				hbSampled.Debug().Msg("heartbeater skipping due to invalid parent addr")
				continue
			}

			// TODO add check for terminate to kill entirely

			if err := vk.HeartbeatParent(); err != nil {
				hbLog.Warn().Uint64("parent ID", p).Str("parent addr", pAddr.String()).Err(err).Msg("failed to heartbeat parent")
				badHBCount += 1
			} else {
				badHBCount = 0
			}

			if badHBCount >= int(vk.hb.badHeartbeatLimit) {
				hbLog.Info().Msg("assuming parent is dead due to consecutive bad heartbeats")
				vk.structure.mu.Lock()
				// only alter the parent if our data is still valid
				if vk.structure.parentID == p && vk.structure.parentAddr == pAddr {
					vk.structure.parentID = 0
					vk.structure.parentAddr = netip.AddrPort{}
				} else {
					hbLog.Info().
						Uint64("parent ID", vk.structure.parentID).
						Uint64("cached parent ID", p).
						Str("parent addr", vk.structure.parentAddr.String()).
						Str("cached parent addr", pAddr.String()).
						Msg("parent data is stale. Heartbeater refrained from pruning parent.")
				}
				vk.structure.mu.Unlock()
				// clear the count
				badHBCount = 0
			}
		}

	}
}

//#region getters

// ID returns the unique ID of this vaultkeeper.
func (vk *VaultKeeper) ID() slims.NodeID {
	return vk.id
}

// Height returns the current height of the vaultkeeper.
// ! Acquires read lock on structure.
func (vk *VaultKeeper) Height() uint16 {
	vk.structure.mu.RLock()
	defer vk.structure.mu.RUnlock()
	return vk.structure.height
}

// Address returns the address+port of the vaultkeeper.
func (vk *VaultKeeper) Address() netip.AddrPort {
	return vk.addr
}

//#endregion getters

// respondError is a helper function to generate a FAULT response and write it across the wire to the given address.
func (vk *VaultKeeper) respondError(addr net.Addr, reason string) {
	// compose the response
	b, err := protocol.Serialize(vk.versionSet.HighestSupported(), false, mt.Fault, vk.id,
		&pb.Fault{Reason: reason})
	if err != nil {
		vk.log.Error().Err(err).Msg("failed to serialize response header")
		return
	} else if len(b) > int(slims.MaxPacketSize) {
		vk.log.Warn().Msg("Orv packet is greater than max packet size. It may be truncated on receipt.")
	}

	if n, err := vk.net.pconn.WriteTo(b, addr); err != nil {
		vk.log.Warn().Err(err).Str("target address", addr.String()).Msg("failed to respond")
	} else if n != len(b) {
		vk.log.Warn().
			Int("total bytes written", n).
			Int("header length (Bytes)", len(b)).
			Int("body length (Bytes)", len([]byte(reason))).
			Msg("bytes written does not equal sum of header and body")
	}

}

// respondSuccess is a helper function to coalesce the given data into a wire-ready format and send it to the target address.
// Logs errors to vk.log instead of returning them.
func (vk *VaultKeeper) respondSuccess(addr net.Addr, hdr protocol.Header, payload proto.Message) {
	hdrB, err := hdr.Serialize()
	if err != nil {
		vk.log.Error().Err(err).Str("target address", addr.String()).Msg("failed to serialize header")
	}
	payloadB, err := proto.Marshal(payload)
	if err != nil {
		vk.log.Error().Err(err).Str("target address", addr.String()).Msg("failed to marshal payload")
	}
	if n, err := vk.net.pconn.WriteTo(append(hdrB, payloadB...), addr); err != nil {
		vk.log.Warn().Err(err).Str("response message type", hdr.Type.String()).Str("target address", addr.String()).Msg("failed to respond")
	} else if n != (len(hdrB) + len(payloadB)) {
		vk.log.Warn().
			Int("total bytes written", n).
			Int("header length (Bytes)", len(hdrB)).
			Int("body length (Bytes)", len(payloadB)).
			Msg("bytes written does not equal sum of header and body")
	}
}

// Start causes the server to begin listening.
// Ineffectual if already listening.
func (vk *VaultKeeper) Start() error {
	if swapped := vk.net.accepting.CompareAndSwap(false, true); !swapped { // mark us as alive; quit if we were already alive
		return nil
	}
	// ! context, cancellation, and pconn are rebuilt on each start up

	// create a context so we can kill this listener instance
	vk.net.ctx, vk.net.cancel = context.WithCancel(context.Background())

	// spin up a packet connection
	if pconn, err := (&net.ListenConfig{}).ListenPacket(vk.net.ctx, "udp", vk.addr.String()); err != nil {
		return err
	} else {
		vk.net.pconn = pconn
	}

	vk.log.Info().Str("local address", vk.addr.String()).Msg("accepting incoming packets")
	go vk.dispatch(vk.net.ctx)

	time.Sleep(30 * time.Millisecond) // buy time for the server to actually start up
	return nil
}

// dispatch handles incoming UDP packets and dispatches a goroutine to handle each.
// Dies when the given context is Done.
// Spun up by .Start(), shuttered by .Stop().
func (vk *VaultKeeper) dispatch(ctx context.Context) {
	// slurp the packet and pass it to the handler func
	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, senderAddr, hdr, body, err := protocol.ReceivePacket(vk.net.pconn, vk.net.ctx)
			if n == 0 {
				vk.log.Debug().Msg("zero byte message received")
				continue
			} else if err != nil {
				vk.log.Warn().Err(err).Msg("receive packet error")
			}
			vk.log.Debug().
				Str("sender address", senderAddr.String()).
				Int("message size (bytes)", n).
				Func(hdr.Zerolog).
				Msg("packet received")
			go func() {
				// TODO increment waitgroup

				// switch on request type.
				// Each sub-handler is expected to respond on its own.
				switch hdr.Type {
				case mt.Hello:
					vk.serveHello(hdr, body, senderAddr)
				case mt.Join:
					vk.serveJoin(hdr, body, senderAddr)
				case mt.Register:
					vk.serveRegister(hdr, body, senderAddr)
				// heartbeats
				case mt.VKHeartbeat:
					vk.serveVKHeartbeat(hdr, body, senderAddr)
				case mt.ServiceHeartbeat:
					vk.serveServiceHeartbeat(hdr, body, senderAddr)
				// client requests that do not require a handshake
				case mt.Status:
					vk.serveStatus(hdr, body, senderAddr)
				default: // non-enumerated type or UNKNOWN
					vk.respondError(senderAddr, "unknown message type "+strconv.FormatUint(uint64(hdr.Type), 10))
					return
				}
			}()
		}
	}
}

// Stop causes the server to stop accepting requests.
// Ineffectual if not listening.
func (vk *VaultKeeper) Stop() {
	if !vk.net.accepting.CompareAndSwap(true, false) {
		return
	}
	// ! context, cancellation, and pconn are rebuilt on each start up

	vk.log.Info().Msg("initializing graceful shutdown")
	if vk.net.cancel != nil {
		vk.net.cancel()
	}
	// do not nil the context or dispatch will attempt to wait on a nil channel.
	// instead, allow cancel to close the channel and the next .Start() to overwrite the net.ctx reference
	// TODO await all handlers
	if err := vk.net.pconn.Close(); err != nil {
		vk.log.Warn().Err(err).Msg("completed shutting down with an error")
	} else {
		vk.log.Info().Msg("completed graceful shutdown")
	}

}

// Zerolog pretty prints the state of the vk into the given zerolog event.
// Intended to be given to *zerolog.Event.Func().
func (vk *VaultKeeper) Zerolog(e *zerolog.Event) {
	e.Uint64("vkid", vk.id).
		Uint16("height", vk.structure.height).
		Str("address", vk.addr.String())
	vk.structure.mu.RLock()
	e.Uint16("height", vk.structure.height).
		Uint64("parent id", vk.structure.parentID).
		Str("parent address", vk.structure.parentAddr.String())
	vk.structure.mu.RUnlock()

	/*vk.children.mu.Lock()
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
	}*/
}

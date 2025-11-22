// Package vaultkeeper implements a vaultkeeper, the primary node of a vault.
// A vaultkeeper can be spun up with New and directed using the subroutines in requests.go.
package vaultkeeper

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"github.com/rflandau/expiring"
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
	// unmarshaller used by VKs to handle incoming message bodies
	pbun = proto.UnmarshalOptions{
		DiscardUnknown: false,
	}
)

// PBUnmarshaller returns the unmarshal options used by VKs to decode incoming bodies.
// The unmarshaller is not settable; this is just for informational purposes.
func PBUnmarshaller() proto.UnmarshalOptions {
	return pbun
}

//#endregion defaults

// A VaultKeeper (VK) is a node with organizational and routing capability.
// The key building block of a Vault.
type VaultKeeper struct {
	log  *zerolog.Logger
	id   slims.NodeID   // unique identifier of this node
	addr netip.AddrPort // address this vk accepts packets on
	net  struct {
		mu        sync.RWMutex       // must be held to interact with this struct
		accepting bool               // are we currently accepting connections?
		pconn     net.PacketConn     // the packet-oriented UDP connection we are listening on
		ctx       context.Context    // the context pconn is running under
		cancel    context.CancelFunc // callable to kill ctx
	}

	versionSet version.Set

	// vault information relevant to us
	structure struct {
		mu         sync.RWMutex   // must be held to interact with this struct
		height     uint16         // current node height
		parentID   slims.NodeID   // 0 if we are root
		parentAddr netip.AddrPort // invalid if we are root
	}

	// how quickly are pieces of data pruned
	pruneTime PruneTimes

	// direct children of this node.
	// NOTE(rlandau): leaves are mildly too complex to be represented by a single expiring table.
	children struct {
		mu sync.Mutex // must be held to interact with this struct

		// child vks
		cvks *expiring.Table[slims.NodeID, struct {
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
	pendingHellos *expiring.Table[slims.NodeID, bool]
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
		badHeartbeatCount atomic.Uint32
	}

	// expiring hashset of tokens that we have seen and handled a LIST request for.
	// Requests that come in for these tokens are considered to be duplicated.
	closedListTokens *expiring.Table[string, bool]
}

// New generates a new VK instance, optionally modified with opts.
// The returned VK is ready for use as soon as it is .Start()'d.
func New(id uint64, addr netip.AddrPort, opts ...VKOption) (*VaultKeeper, error) {
	if !addr.IsValid() {
		return nil, ErrBadAddr(addr)
	}

	// set defaults
	vk := &VaultKeeper{
		id:   id,
		addr: addr,
		net: struct {
			mu        sync.RWMutex
			accepting bool
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
		pruneTime: PruneTimes{
			Hello:           DefaultHelloPruneTime,
			ServicelessLeaf: DefaultServicelessLeafPruneTime,
			ChildVK:         DefaultHeartbeatlessCVKPruneTime,
		},
		children: struct {
			mu   sync.Mutex
			cvks *expiring.Table[slims.NodeID, struct {
				services map[string]netip.AddrPort
				addr     netip.AddrPort
			}]
			leaves      map[slims.NodeID]leaf
			allServices map[string]map[slims.NodeID]netip.AddrPort
		}{
			cvks: expiring.NewTable[slims.NodeID, struct {
				services map[string]netip.AddrPort
				addr     netip.AddrPort
			}](),
			leaves:      make(map[slims.NodeID]leaf),
			allServices: make(map[string]map[slims.NodeID]netip.AddrPort),
		},
		pendingHellos: expiring.NewTable[slims.NodeID, bool](),
		hb: struct {
			// do we send heartbeats automatically?
			// Defaults to true.
			auto bool
			// how often do we send heartbeats
			freq time.Duration
			// when we fail to heartbeat our parent this many times consecutively, the parent will be dropped
			badHeartbeatLimit uint
			badHeartbeatCount atomic.Uint32
		}{
			auto:              true,
			freq:              DefaultParentHBFreq,
			badHeartbeatLimit: DefaultBadHeartbeatLimit,
			badHeartbeatCount: atomic.Uint32{},
		},
		closedListTokens: expiring.NewTable[string, bool](),
	}

	// apply options
	for _, opt := range opts {
		opt(vk)
	}

	// if the logger was not established by the options, generate the default logger
	if vk.log == nil {
		l := zerolog.New(zerolog.ConsoleWriter{
			Out:         os.Stdout,
			FieldsOrder: []string{"vkid", "type"},
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
		for {
			<-tkr.C
			hbSampled.Debug().Msg("heartbeater waking")
			vk.net.mu.RLock()
			if !vk.net.accepting {
				vk.net.mu.RUnlock()
				continue
			}
			vk.net.mu.RUnlock()
			// cache parent information
			vk.structure.mu.RLock()
			p, pAddr := vk.structure.parentID, vk.structure.parentAddr
			vk.structure.mu.RUnlock()

			if !pAddr.IsValid() {
				hbSampled.Debug().Msg("heartbeater skipping due to invalid parent addr")
				vk.hb.badHeartbeatCount.Store(0)
				continue
			}

			if err := vk.HeartbeatParent(); err != nil {
				hbLog.Warn().Uint64("parent ID", p).Str("parent addr", pAddr.String()).Err(err).Msg("failed to heartbeat parent")
				vk.hb.badHeartbeatCount.Add(1)
			} else {
				vk.hb.badHeartbeatCount.Store(0)
			}

			if int(vk.hb.badHeartbeatCount.Load()) >= int(vk.hb.badHeartbeatLimit) {
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
				vk.hb.badHeartbeatCount.Store(0)
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
// origMT is the type of the message that triggered the fault.
// errno is the enumerated fault number that, when combined with origMT, points to a specific error.
// extraInfo is an optional list of lines (that will be imploded with '\n' to form a single line) to stuff in additional_info.
func (vk *VaultKeeper) respondError(addr net.Addr, origMT pb.MessageType, errno pb.Fault_Errnos, extraInfo ...string) {
	fault := &pb.Fault{
		Original: origMT,
		Errno:    pb.Fault_Errnos(errno),
	}
	baseLen := proto.Size(fault)
	if len(extraInfo) > 0 {
		ai := strings.Join(extraInfo, "\n")
		// length-check the additional info before attaching it
		if total := baseLen + len(ai); total > int(slims.MaxPacketSize) {
			// truncate until it fits
			vk.log.Warn().
				Str("original message type", origMT.String()).
				Str("errno", fault.Errno.String()).
				Str("extra info", ai).
				Uint16("max packet size", slims.MaxPacketSize).
				Int("number of bytes truncated", total-int(slims.MaxPacketSize)).
				Msg("fault message is greater than the max packet size; extra info will be truncated.")
			ai = ai[:total-int(slims.MaxPacketSize)]
		}
		fault.AdditionalInfo = &ai
	}
	// compose the response
	b, err := protocol.Serialize(vk.versionSet.HighestSupported(), false, pb.MessageType_FAULT, vk.id, fault)
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
			Int("packet length (bytes)", len(b)).
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
	vk.net.mu.Lock()
	defer vk.net.mu.Unlock()
	if vk.net.accepting {
		return nil
	}
	vk.net.accepting = true
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

// A MessageHandler is a function that handles responding to an incoming Orv message.
// MessageHandlers return error information if one occurred, but are expected to craft their own success responses (iff !errored).
// dispatch() is responsible for calling the handlers and sending an error response if applicable.
type MessageHandler func(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string)

// dispatch handles incoming UDP packets and dispatches a goroutine to handle each.
// Dies when the given context is Done.
// Spun up by .Start(), shuttered by .Stop().
func (vk *VaultKeeper) dispatch(ctx context.Context) {
	// generate the list of handlers
	var handlers = map[pb.MessageType]MessageHandler{
		pb.MessageType_HELLO:             vk.serveHello,
		pb.MessageType_JOIN:              vk.serveJoin,
		pb.MessageType_MERGE:             vk.serveMerge,
		pb.MessageType_INCREMENT:         vk.serveIncrement,
		pb.MessageType_LEAVE:             vk.serveLeave,
		pb.MessageType_REGISTER:          vk.serveRegister,
		pb.MessageType_DEREGISTER:        vk.serveDeregister,
		pb.MessageType_SERVICE_HEARTBEAT: vk.serveServiceHeartbeat,
		pb.MessageType_VK_HEARTBEAT:      vk.serveVKHeartbeat,

		// client requests
		pb.MessageType_STATUS: vk.serveStatus,
		pb.MessageType_LIST:   vk.serveList,
		pb.MessageType_GET:    vk.serveGet,
	}

	// slurp the packet and pass it to the handler func
	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, senderAddr, hdr, body, err := protocol.ReceivePacket(vk.net.pconn, vk.net.ctx)
			if err != nil {
				vk.log.Warn().Err(err).Msg("receive packet error")
				continue
			} else if n == 0 {
				vk.log.Debug().Msg("zero byte message received")
				continue
			} else if hdr.ID == vk.id {
				vk.log.Warn().Str("header", hdr.String()).Msg("message has our ID; ignoring.")
				continue
			}
			go func() {
				mh, found := handlers[hdr.Type]
				if !found {
					vk.respondError(senderAddr, hdr.Type, pb.Fault_UNKNOWN_TYPE)
					return
				}
				vk.log.Info().
					Str("sender address", senderAddr.String()).
					Int("message size (bytes)", n).
					Func(hdr.Zerolog).
					Msg("packet received")
				if erred, errno, ei := mh(hdr, body, senderAddr); erred {
					vk.respondError(senderAddr, hdr.Type, errno, ei...)
				}
			}()
		}
	}
}

// Stop causes the server to stop accepting requests.
// Ineffectual if not listening.
func (vk *VaultKeeper) Stop() {
	vk.net.mu.Lock()
	defer vk.net.mu.Unlock()
	if !vk.net.accepting {
		return
	}
	vk.net.accepting = false
	// ! context, cancellation, and pconn are rebuilt on each start up

	vk.log.Info().Msg("initializing graceful shutdown")
	if vk.net.cancel != nil {
		vk.net.cancel()
	}
	// do not nil the context or dispatch will attempt to wait on a nil channel.
	// instead, allow cancel to close the channel and the next .Start() to overwrite the net.ctx reference
	if err := vk.net.pconn.Close(); err != nil {
		vk.log.Warn().Err(err).Msg("completed shutting down with an error")
	} else {
		vk.log.Info().Msg("completed graceful shutdown")
	}

}

// Zerolog pretty prints the state of the vk into the given zerolog event.
// Intended to be given to *zerolog.Event.Func().
// Uses vk.Snapshot() under the hood.
func (vk *VaultKeeper) Zerolog(e *zerolog.Event) {
	snap := vk.Snapshot()

	e.Uint64("vkid", snap.ID).
		Uint16("height", snap.Height).
		Str("address", snap.Addr.String()).
		Bool("accepting connections?", snap.AcceptingConnections).
		Any("supported versions", snap.Versions)
	if snap.ParentAddr.IsValid() {
		e.Uint64("parent ID", snap.ParentID).Str("parent address", snap.ParentAddr.String())
	} else {
		e.Bool("is root?", true)
	}
	e.Any("prune times", snap.PruneTimes).
		Any("child VKs", snap.Children.CVKs).
		Any("leaves", snap.Children.Leaves)
	e.Bool("auto heartbeater enabled?", snap.AutoHeartbeater.Enabled).
		Dur("heartbeat frequency", snap.AutoHeartbeater.Frequency).
		Uint("bad heartbeat limit", snap.AutoHeartbeater.Limit)
}

type VKSnapshot struct {
	ID                   slims.NodeID   // unique ID
	Addr                 netip.AddrPort // address listening at
	AcceptingConnections bool           // is the VK current accepting Orv connections/requests?
	Versions             version.Set    // supported versions
	Height               uint16
	ParentID             slims.NodeID   // ID of this node's parent. If ParentAddr is not valid, then this should be ignored.
	ParentAddr           netip.AddrPort // Address of this node's parent's Orv endpoint
	PruneTimes           PruneTimes
	Children             struct {
		CVKs map[slims.NodeID]struct {
			Services map[string]netip.AddrPort // service name -> service address
			Addr     netip.AddrPort            // Orv endpoint the child can receive Orv requests on
		} // cvkID -> ((service name -> service address), cvk Orv address)
		Leaves map[slims.NodeID]map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		} // leafID -> (service name -> (stale, service address))
	}
	AutoHeartbeater struct {
		Enabled   bool // is the autohb running?
		Frequency time.Duration
		Limit     uint
	}
}

// Snapshot returns a point-in-time snapshot of the vk at call time.
// All locks are acquired before data is gathered and released on returned.
// Data is not guaranteed to be entirely consistent due to the use of multiple locks across multiple structs.
//
// Other nodes should prefer sending STATUS packets to retrieve metadata.
//
// If you are using zerolog, call vk.Zerolog() instead, as that will properly format this data into a single log item.
func (vk *VaultKeeper) Snapshot() VKSnapshot {
	// acquire all locks
	vk.net.mu.RLock()
	defer vk.net.mu.RUnlock()
	vk.structure.mu.RLock()
	defer vk.structure.mu.RUnlock()
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()
	snap := VKSnapshot{
		ID:                   vk.id,
		Addr:                 vk.addr,
		AcceptingConnections: vk.net.accepting,
		Versions:             vk.versionSet,
		Height:               vk.structure.height,
		ParentID:             vk.structure.parentID,
		ParentAddr:           vk.structure.parentAddr,
		PruneTimes:           vk.pruneTime,
		Children: struct {
			CVKs map[slims.NodeID]struct {
				Services map[string]netip.AddrPort
				Addr     netip.AddrPort
			}
			Leaves map[slims.NodeID]map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}
		}{},
		AutoHeartbeater: struct {
			Enabled   bool
			Frequency time.Duration
			Limit     uint
		}{Enabled: vk.hb.auto, Frequency: vk.hb.freq, Limit: vk.hb.badHeartbeatLimit},
	}
	// attach children
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		snap.Children.CVKs = make(map[slims.NodeID]struct {
			Services map[string]netip.AddrPort
			Addr     netip.AddrPort
		})
		vk.children.cvks.RangeLocked(func(ni slims.NodeID, s struct {
			services map[string]netip.AddrPort
			addr     netip.AddrPort
		}) bool {
			// add the pair to the table
			snap.Children.CVKs[ni] = struct {
				Services map[string]netip.AddrPort
				Addr     netip.AddrPort
			}{s.services, s.addr}
			return true
		})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		snap.Children.Leaves = make(map[slims.NodeID]map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}, len(vk.children.leaves))
		for id, l := range vk.children.leaves {
			services := make(map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}, len(l.services))
			for service, info := range l.services { // transmogrify each service
				services[service] = struct {
					Stale time.Duration
					Addr  netip.AddrPort
				}{info.stale, info.addr}
			}
			snap.Children.Leaves[id] = services
		}
	}()
	wg.Wait()
	return snap
}

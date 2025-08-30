// Package vaultkeeper implements a vaultkeeper, the primary node of a vault.
// A vaultkeeper can be spun up with New and directed using the subroutines in requests.go.
package vaultkeeper

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"github.com/rflandau/Orv/implementations/slims/slims/vk/expiring"
	"github.com/rs/zerolog"
)

// A VaultKeeper (VK) is a node with organizational and routing capability.
// The key building block of a Vault.
type VaultKeeper struct {
	//alive atomic.Bool // has this VK been terminated?
	log  *zerolog.Logger
	id   slims.NodeID // unique identifier of this node
	addr netip.AddrPort
	net  struct {
		accepting atomic.Bool        // are we currently accepting connections?
		pconn     net.PacketConn     // the packet-oriented UDP connection we are listening on
		ctx       context.Context    // the context pconn is running under
		cancel    context.CancelFunc // callable to kill ctx
	}

	structure struct {
		mu         sync.RWMutex // lock that must be held to interact with the fields of structure
		height     uint16       // current node height
		parentID   slims.NodeID // 0 if we are root
		parentAddr netip.AddrPort
	}

	pendingHellos expiring.Table[slims.NodeID, bool]
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
		structure: struct {
			mu         sync.RWMutex
			height     uint16
			parentID   uint64
			parentAddr netip.AddrPort
		}{},
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
			Logger().Level(zerolog.WarnLevel)
		vk.log = &l
	}

	vk.log.Debug().Func(vk.Zerolog).Msg("vk created")

	return vk, nil

}

//#region getters

// ID returns the unique ID of this vaultkeeper.
func (vk *VaultKeeper) ID() slims.NodeID {
	return vk.id
}

// Height returns the current height of the vaultkeeper.
func (vk *VaultKeeper) Height() uint16 {
	vk.structure.mu.RLock()
	defer vk.structure.mu.RUnlock()
	return vk.structure.height
}

//#endregion getters

// respondError is a helper function to generate a FAULT response and write it across the wire to the given address.
func (vk *VaultKeeper) respondError(addr net.Addr, reason string) {
	// compose the response
	b, err := protocol.Serialize(protocol.HighestSupported, false, mt.Fault, vk.id, &pb.Fault{Reason: reason})
	if err != nil {
		vk.log.Error().Err(err).Msg("failed to serialize response header")
		return
	} else if len(b) > int(slims.MaxPacketSize) {
		vk.log.Warn().Msg("Orv packet is greater than max packet size. It may be truncated on receipt.")
	}

	if n, err := vk.net.pconn.WriteTo(append(b, []byte(reason)...), addr); err != nil {
		vk.log.Warn().Err(err).Str("target address", addr.String()).Msg("failed to respond")
	} else if n != (len(b) + len([]byte(reason))) {
		vk.log.Warn().
			Int("total bytes written", n).
			Int("header length (Bytes)", len(b)).
			Int("body length (Bytes)", len([]byte(reason))).
			Msg("bytes written does not equal sum of header and body")
	}

}

func (vk *VaultKeeper) respondSuccess(addr net.Addr, hdr *protocol.Header, body []byte) {
	hdrB, err := hdr.Serialize()
	if err != nil {
		vk.log.Error().Err(err).Str("target address", addr.String()).Msg("failed to serialize header")
	}
	if n, err := vk.net.pconn.WriteTo(append(hdrB, body...), addr); err != nil {
		vk.log.Warn().Err(err).Str("response message type", hdr.Type.String()).Str("target address", addr.String()).Msg("failed to respond")
	} else if n != (len(hdrB) + len(body)) {
		vk.log.Warn().
			Int("total bytes written", n).
			Int("header length (Bytes)", len(hdrB)).
			Int("body length (Bytes)", len(body)).
			Msg("bytes written does not equal sum of header and body")
	}
}

// Start causes the server to begin listening.
// Ineffectual if already listening.
func (vk *VaultKeeper) Start() error {
	if swapped := vk.net.accepting.CompareAndSwap(false, true); !swapped { // mark us as alive; quit if we were already alive
		return nil
	}

	// create a context so we can kill this listener instance
	vk.net.ctx, vk.net.cancel = context.WithCancel(context.Background())

	// spin up a packet connection
	if pconn, err := (&net.ListenConfig{}).ListenPacket(vk.net.ctx, "udp", vk.addr.String()); err != nil {
		return err
	} else {
		vk.net.pconn = pconn
	}

	vk.log.Info().Str("local address", vk.addr.String()).Msg("accepting incoming packets")
	go vk.dispatch()

	time.Sleep(30 * time.Millisecond) // buy time for the server to actually start up
	return nil
}

// dispatch handles incoming UDP packets and dispatches a handler for each.
// Spun up by .Start(), shuttered by .Stop().
func (vk *VaultKeeper) dispatch() {
	func() { // slurp the packet and pass it to the handler func
		for {
			var pktbuf = make([]byte, slims.MaxPacketSize)
			rxN, senderAddr, err := vk.net.pconn.ReadFrom(pktbuf)
			// TODO add a check on alive or context's done chan
			if rxN == 0 {
				vk.log.Debug().Msg("zero byte message received")
				continue
			} else if err != nil {
				vk.log.Warn().Err(err).Msg("packet read error, returning...")
				return
			}
			vk.log.Debug().Str("sender address", senderAddr.String()).Int("message size (bytes)", rxN).Msg("packet received")
			go vk.handle(pktbuf, senderAddr)
		}

	}()
}

// Stop causes the server to stop accepting requests.
// Ineffectual if not listening.
func (vk *VaultKeeper) Stop() {
	if !vk.net.accepting.CompareAndSwap(true, false) {
		return
	}

	// TODO do we also need to send a stop to pconn directly or is the context enough?
	vk.log.Info().Msg("initializing graceful shutdown")
	if vk.net.cancel != nil {
		vk.net.cancel()
	}
	pconnCloseErr := vk.net.pconn.Close()
	// TODO await all handlers
	vk.net.ctx = nil
	vk.log.Info().AnErr("conn close error", pconnCloseErr).Msg("completed graceful shutdown")

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

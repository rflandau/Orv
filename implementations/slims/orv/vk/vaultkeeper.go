// TODO annotate package
package vaultkeeper

import (
	"bytes"
	"net/netip"
	"network-bois-orv/implementations/slims/orv/proto"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/server"
	"github.com/rs/zerolog"
)

// A VaultKeeper (VK) is a node with organizational and routing capability.
// The key building block of a Vault.
type VaultKeeper struct {
	alive atomic.Bool // has this VK been terminated?
	log   *zerolog.Logger
	id    uint64 // unique identifier of this node
	addr  netip.AddrPort
	net   struct {
		alive    atomic.Bool // are we currently accepting connections?
		listener *net.UDPConn
		server   *server.Server
	}
	mux *mux.Router

	structure struct {
		mu         sync.RWMutex // lock that must be held to interact with the fields of structure
		height     uint16       // current node height
		parentID   uint64       // 0 if we are root
		parentAddr netip.AddrPort
	}
}

// VKOption function to set various options on the vault keeper.
// Uses defaults if an option is not set.
type VKOption func(*VaultKeeper)

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
			alive    atomic.Bool
			listener *net.UDPConn
			server   *server.Server
		}{},
		structure: struct {
			mu         sync.RWMutex
			height     uint16
			parentID   uint64
			parentAddr netip.AddrPort
		}{},
		// TODO set default prune times and frequencies
	}
	vk.alive.Store(true)

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
			Uint64("vk", vk.id).
			Timestamp().
			Caller().
			Logger().Level(zerolog.WarnLevel)
		vk.log = &l
	}

	// if a muxer was not established by the options, generate one with default handling
	if vk.mux == nil {
		vk.mux = mux.NewRouter()
		vk.mux.Use(func(next mux.Handler) mux.Handler { // install logging middleware
			return mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
				sz, szErr := r.BodySize()
				mt, mtErr := r.ContentFormat()
				path, pathErr := r.Path()
				vk.log.Debug().
					Str("token", r.Token().String()).
					Str("code", r.Code().String()).
					Uint64("sequence", r.Sequence()).
					Int64("body size", sz).AnErr("body size", szErr).
					Str("media type", mt.String()).AnErr("media type", mtErr).
					Str("path", path).AnErr("path", pathErr).
					Msg("request received")
				next.ServeCOAP(w, r)
			})
		})
		vk.mux.Handle("/", mux.HandlerFunc(vk.handler))
	}

	// generate a child handler
	// TODO

	// spawn a pruner service
	// TODO

	// spawn the heartbeater service
	// TODO

	vk.log.Debug().Func(vk.LogDump).Msg("vk created")

	return vk, nil

}

// respondError is a helper function that sets the response on the given writer and logs if SetResponse fails.
// Responses contain the given code and message and are written as plain text.
func (vk *VaultKeeper) respondError(resp mux.ResponseWriter, code codes.Code, msg string) {
	if err := resp.SetResponse(code, message.TextPlain, strings.NewReader(msg)); err != nil {
		vk.log.Error().Str("body", msg).Err(err).Msg("failed to set response")
	}
}

// respondSuccess is a helper function that sets the response on the given writer and logs if SetResponse fails.
// Responses contain the given code and ReadSeeker and are written as an OctetStream.
func (vk *VaultKeeper) respondSuccess(resp mux.ResponseWriter, code codes.Code, hdr proto.Header, body []byte) {
	hdrB, err := hdr.Serialize()
	if err != nil {
		vk.log.Error().Msg("failed to serialize header")
		return
	}
	if err := resp.SetResponse(code, message.TextPlain, bytes.NewReader(append(hdrB, body...))); err != nil {
		vk.log.Error().
			Bytes("body", body).
			Str("body string", string(body)).
			Err(err).
			Msg("failed to set response")
	}
}

// Start causes the server to begin listening.
// Ineffectual if already listening.
func (vk *VaultKeeper) Start() {
	if !vk.net.alive.CompareAndSwap(false, true) { // mark us as alive; quite if we were already alive
		return
	}
	// spawn a server and listener
	vk.net.server = udp.NewServer(options.WithMux(vk.mux))
	var err error
	vk.net.listener, err = net.NewListenUDP("udp", vk.addr.String())
	if err != nil {
		//return err
	}
	vk.log.Info().Msg("starting server...")
	go func() {
		if err := vk.net.server.Serve(vk.net.listener); err != nil {
			vk.log.Info().Err(err).Msg("server outcome")
		}

	}()

	time.Sleep(5 * time.Millisecond) // buy time for the server to actually start up
}

// Stop causes the server to stop accepting requests.
// Ineffectual if not listening.
func (vk *VaultKeeper) Stop() {
	if !vk.net.alive.CompareAndSwap(true, false) {
		return
	}
	vk.log.Info().Msg("shuttering server...")

	vk.net.server.Stop()
	vk.net.server = nil
	vk.net.listener.Close()
	vk.net.listener = nil
}

// Pretty prints the state of the vk into the given zerolog event.
// Used for debugging purposes.
func (vk *VaultKeeper) LogDump(e *zerolog.Event) {
	e.Uint16("height", vk.structure.height)
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

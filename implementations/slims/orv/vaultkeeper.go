package orv

import (
	"net/netip"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// A VaultKeeper (VK) is a node with organizational and routing capability.
// The key building block of a Vault.
type VaultKeeper struct {
	alive atomic.Bool // has this VK been terminated?
	log   *zerolog.Logger
	id    uint64 // unique identifier of this node
	addr  netip.AddrPort

	structure struct {
		mu         sync.RWMutex // lock that must be held to interact with the fields of structure
		height     uint16       // current node height
		parentID   uint64       // 0 if we are riit
		parentAddr netip.AddrPort
	}
}

// Function to set various options on the vault keeper.
// Uses defaults if an option is not set.
type VKOption func(*VaultKeeper)

// NewVaultKeeper generates a new VK instance, optionally modified with opts.
// The returned VK is ready for use as soon as it is .Start()'d.
func NewVaultKeeper(id uint64, addr netip.AddrPort, opts ...VKOption) (*VaultKeeper, error) {
	if !addr.IsValid() {
		return nil, ErrBadAddr(addr)
	}

	// set defaults

	vk := &VaultKeeper{
		id:   id,
		addr: addr,
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

	// spawn a listener on the address to receive packets
	// TODO

	// generate a child handler
	// TODO

	// spawn a pruner service
	// TODO

	// spawn the heartbeater service
	// TODO

	vk.log.Debug().Func(vk.LogDump).Msg("vk created")

	return vk, nil

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

package orv

import (
	"github.com/rs/zerolog"
)

/*
A single instance of a Vault Keeper.
Able to provide services, route & answer requests, and facilitate vault growth.
*/
type VaultKeeper struct {
	log zerolog.Logger // output logger
	id  uint64         // id is assumed to be IPv6 for the prototype
	// services
	children struct {
		id       uint64
		services map[string]string // service name -> endpoint
	}
}

// Spawns and returns a new vault keeper instance
func (vk *VaultKeeper) New(id uint64) {

}

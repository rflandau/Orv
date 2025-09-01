package vaultkeeper

import (
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"github.com/rs/zerolog"
)

// File options.go provides options that can be passed to the vaultkeeper constructor to configure it.

// VKOption function to set various options on the vault keeper.
// Uses defaults if an option is not set.
type VKOption func(*VaultKeeper)

// WithLogger replaces the vk's default logger with the given logger.
func WithLogger(l *zerolog.Logger) VKOption {
	return func(vk *VaultKeeper) {
		vk.log = l
	}
}

// WithDragonsHoard starts the vk "with a hoard" (aka an initial height, instead of 0).
func WithDragonsHoard(initialHeight uint16) VKOption {
	return func(vk *VaultKeeper) {
		vk.structure.mu.Lock()
		vk.structure.height = initialHeight
		vk.structure.mu.Unlock()
	}
}

// WithVersions specifies the versions available to the vk, overwriting those provided by the protocol package.
// ! For testing purposes ONLY.
func WithVersions(s version.Set) VKOption {
	return func(vk *VaultKeeper) {
		vk.versionSet = s
	}
}

// PruneTimes can be used to configure the time before records are pruned out of a vaultkeeper.
type PruneTimes struct {
	Hello           time.Duration // how long should a hello stay in the pending table (which is required for a follow-up JOIN)
	ServicelessLeaf time.Duration
	ChildVK         time.Duration // how long can a childVK not send a heartbeat before it is considered stale
}

// WithPruneTimes overwrites DefaultHelloPruneTime.
func WithPruneTimes(pt PruneTimes) VKOption {
	return func(vk *VaultKeeper) {
		if pt.Hello > 0 {
			vk.pruneTime.hello = pt.Hello
		}
		if pt.ChildVK > 0 {
			vk.pruneTime.cvk = pt.ChildVK
		}
		if pt.ServicelessLeaf > 0 {
			vk.pruneTime.servicelessLeaf = pt.ServicelessLeaf
		}
	}
}

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
// If nil is given, a nop logger will be set.
func WithLogger(l *zerolog.Logger) VKOption {
	if l == nil {
		nl := zerolog.Nop()
		l = &nl
	}
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
	ServicelessLeaf time.Duration // how long after join, if no services are registered, will a child leaf be pruned
	ChildVK         time.Duration // how long can a childVK not send a heartbeat before it is considered stale
}

// WithPruneTimes overwrite the default prune timings.
//
// ! Only respects values > 0.
func WithPruneTimes(pt PruneTimes) VKOption {
	return func(vk *VaultKeeper) {
		if pt.Hello > 0 {
			vk.pruneTime.Hello = pt.Hello
		}
		if pt.ChildVK > 0 {
			vk.pruneTime.ChildVK = pt.ChildVK
		}
		if pt.ServicelessLeaf > 0 {
			vk.pruneTime.ServicelessLeaf = pt.ServicelessLeaf
		}
	}
}

// WithCustomHeartbeats allows you to modify the default automatic heartbeat timings or turn them off entirely.
//
//	Enabled sets whether the auto heartbeater runs at all.
//
//	Frequency sets how often the auto heartbeater ticks.
//
//	Limit sets the # of consecutive heartbeat failures before considering the parent dead and dropping them. This is an AT or GREATER limit.
//
// ! If heartbeats are disabled, this VK's parent will prune it unless some other mechanism is set up to send heartbeats on our behalf.
func WithCustomHeartbeats(enabled bool, frequency time.Duration, limit uint) VKOption {
	return func(vk *VaultKeeper) {
		vk.hb.auto = enabled
		if frequency > 0 {
			vk.hb.freq = frequency
		}
		if limit > 0 {
			vk.hb.badHeartbeatLimit = limit
		}
	}
}

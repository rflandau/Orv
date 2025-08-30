package vaultkeeper

import (
	"time"

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

// WithHelloPruneTime overwrites DefaultHelloPruneTime.
func WithHelloPruneTime(t time.Duration) VKOption {
	return func(vk *VaultKeeper) { vk.pruneTime.hello = t }
}

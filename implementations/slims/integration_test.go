package slims_test

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
)

// This file provides external testing to the slims library.
// Lots of overlap with individual unit tests, making this somewhat redundant.

// TestTwoLayerVault spins up a root vk and joins several children under it, then ensures only the ones with no auto-beater get pruned.
func TestTwoLayerVault(t *testing.T) {
	const (
		parentCVKPruneTime = 100 * time.Millisecond
		childHeartbeatFreq = 20 * time.Millisecond
		badHBLimit         = 2
	)

	parent, err := vaultkeeper.New(rand.Uint64(), RandomLocalhostAddrPort(),
		vaultkeeper.WithDragonsHoard(1),
		vaultkeeper.WithPruneTimes(vaultkeeper.PruneTimes{ChildVK: parentCVKPruneTime}))
	if err != nil {
		t.Fatal(err)
	} else if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	// spin up a random number of "good" children
	var goodCVKs = make([]*vaultkeeper.VaultKeeper, rand.UintN(10))
	var wg sync.WaitGroup
	for i := range goodCVKs {
		goodCVKs[i], err = vaultkeeper.New(rand.Uint64(), RandomLocalhostAddrPort())
		if err != nil {
			t.Fatal(err)
		} else if err := goodCVKs[i].Start(); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(cvk *vaultkeeper.VaultKeeper) {
			defer wg.Done()
			// hello
			if pvk, ver, ack, err := client.Hello(t.Context(), cvk.ID(), parent.Address()); err != nil {
				t.Errorf("cvk(%d) failed to HELLO: %v", cvk.ID(), err)
			} else if pvk != parent.ID() {
				t.Error("bad response parent ID", ExpectedActual(parent.ID(), pvk))
			} else if ver != protocol.SupportedVersions().HighestSupported() {
				t.Error("bad version", ExpectedActual(protocol.SupportedVersions().HighestSupported(), ver))
			} else if ack.Height != 1 {
				t.Error("bad response parent height", ExpectedActual(1, ack.Height))
			}
			// join
			if err := cvk.Join(t.Context(), parent.Address()); err != nil {
				t.Errorf("cvk(%d) failed to JOIN: %v", cvk.ID(), err)
			}
		}(goodCVKs[i])
	}
	wg.Wait()
	// check that the parent recognizes all good cvks
	// TODO
	// add a cvk that does not heartbeat and ensure it (and it alone) is pruned
	// TODO
}

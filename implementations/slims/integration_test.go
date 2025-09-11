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
		goodCVKs[i], err = vaultkeeper.New(rand.Uint64(), RandomLocalhostAddrPort(),
			vaultkeeper.WithCustomHeartbeats(true, childHeartbeatFreq, badHBLimit))
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
			t.Logf("cvk (ID%d) joined parent", cvk.ID())
		}(goodCVKs[i])
	}
	wg.Wait()
	t.Logf("added %d cvks.", len(goodCVKs))
	time.Sleep(5 * time.Millisecond)
	// check that the parent recognizes all good cvks
	{
		snap := parent.Snapshot()
		for i, gcvk := range goodCVKs {
			if v, found := snap.Children.CVKs[gcvk.ID()]; !found {
				t.Errorf("failed to find child #%d (ID%d)", i, gcvk.ID())
			} else if len(v.Services) != 0 {
				t.Errorf("expected no services to be registered to child #%d (ID%d). Found: %v", i, gcvk.ID(), v.Services)
			} else if v.Addr != goodCVKs[i].Address() {
				t.Error("bad cvk address", ExpectedActual(goodCVKs[i].Address(), v.Addr))
			}
		}
	}
	// add a cvk that does not heartbeat and ensure it (and it alone) is pruned
	badCVK, err := vaultkeeper.New(rand.Uint64(), RandomLocalhostAddrPort(),
		vaultkeeper.WithCustomHeartbeats(false, 0, 0))
	if err != nil {
		t.Fatal("failed to create bad (heartbeat-less) CVK: ", err)
	} else if err := badCVK.Start(); err != nil {
		t.Fatal("failed to start bad cvk", err)
	}
	if pvk, ver, ack, err := client.Hello(t.Context(), badCVK.ID(), parent.Address()); err != nil {
		t.Errorf("cvk(%d) failed to HELLO: %v", badCVK.ID(), err)
	} else if pvk != parent.ID() {
		t.Error("bad response parent ID", ExpectedActual(parent.ID(), pvk))
	} else if ver != protocol.SupportedVersions().HighestSupported() {
		t.Error("bad version", ExpectedActual(protocol.SupportedVersions().HighestSupported(), ver))
	} else if ack.Height != 1 {
		t.Error("bad response parent height", ExpectedActual(1, ack.Height))
	}
	// join
	if err := badCVK.Join(t.Context(), parent.Address()); err != nil {
		t.Errorf("badCVK(%d) failed to JOIN: %v", badCVK.ID(), err)
	}
	t.Logf("badCVK (ID%d) joined parent", badCVK.ID())
	// check that badCVk is considered a child
	{
		snap := parent.Snapshot()
		if v, found := snap.Children.CVKs[badCVK.ID()]; !found {
			t.Errorf("failed to find bad child (ID%d)", badCVK.ID())
		} else if len(v.Services) != 0 {
			t.Errorf("expected no services to be registered to bad child (ID%d). Found: %v", badCVK.ID(), v.Services)
		} else if v.Addr != badCVK.Address() {
			t.Error("bad cvk address", ExpectedActual(badCVK.Address(), v.Addr))
		}
	}
	// allow bad CVK to expire
	time.Sleep(parentCVKPruneTime)
	// confirm that bad cvk is no longer considered a child
	{
		if _, found := parent.Snapshot().Children.CVKs[badCVK.ID()]; found {
			t.Errorf("bad child (ID%d) has not been pruned", badCVK.ID())
		}
	}
}

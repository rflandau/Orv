package slims_test

// This file provides external testing to the slims library.
// Lots of overlap with individual unit tests, making this somewhat redundant.

import (
	"maps"
	"math/rand/v2"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
	"github.com/rs/zerolog"
)

// HeaderSerialization tests are serviced by implementations/slims/slims/protocol/protocol_test.go (and most thoroughly by TestFullSend()).

// EndpointArgs tests are serviced.... all over the place.
// This implementation does not use endpoints; instead, we switch on packet types. See basically all the tests in implementations/slims/slims/client/client_test.go and all of the Test_serve* tests in  implementations/slims/slims/vk/vaultkeeper_test.go.

// StatusRequest is services via TestVaultKeeper_StartStop in implementations/slims/slims/vk/vaultkeeper_test.go and TestStatus in implementations/slims/slims/client/client_test.go.

type leaf struct {
	id       slims.NodeID
	services map[string]netip.AddrPort // service -> service addr
}

// TestMultiServiceMultiLeaf spins up a single vk and joins multiple leaves under it. Each leaf offers at least one service and at least one service is offered by multiple leaves.
func TestMultiServiceMultiLeaf(t *testing.T) {
	const (
		maxLeaves          uint32 = 25
		maxServicesPerLeaf uint32 = 3

		leafPrune  time.Duration = 100 * time.Millisecond
		leafHBFreq time.Duration = 20 * time.Millisecond
	)
	vk, err := vaultkeeper.New(rand.Uint64(), RandomLocalhostAddrPort(),
		vaultkeeper.WithPruneTimes(vaultkeeper.PruneTimes{
			Hello:           3 * time.Second,        // functionally disabled
			ServicelessLeaf: 200 * time.Millisecond, // to prove that heartbeats actually work
			ChildVK:         3 * time.Second,        // irrelevant for this test
		}), vaultkeeper.WithLogger(&zerolog.Logger{}))
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	// spawn a bunch of (at least 2) leaves and channels to catch heartbeat errors from them.
	var (
		leaves = make([]leaf, rand.Uint32N(maxLeaves)+2)
		hbErrs = make([]chan error, len(leaves)) // initialized with each leaf
	)

	for i := range leaves {
		hbErrs[i] = make(chan error, 10)
		serviceCount := rand.Uint32N(maxServicesPerLeaf + 1)
		leaves[i] = leaf{
			id:       rand.Uint64(),
			services: make(map[string]netip.AddrPort, serviceCount),
		}

		// attach it to the vk
		parentID, _, ack, err := client.Hello(t.Context(), leaves[i].id, vk.Address())
		if err != nil {
			t.Fatal(err)
		}
		if parentID != vk.ID() {
			t.Error(ExpectedActual(vk.ID(), parentID))
		}
		if ack.Height != uint32(vk.Height()) {
			t.Error(ExpectedActual(uint32(vk.Height()), ack.Height))
		}
		parentID, accept, err := client.Join(t.Context(), leaves[i].id, vk.Address(), client.JoinInfo{IsVK: false, VKAddr: netip.AddrPort{}, Height: 0})
		if err != nil {
			t.Fatal(err)
		}
		if parentID != vk.ID() {
			t.Error(ExpectedActual(vk.ID(), parentID))
		}
		if accept.Height != uint32(vk.Height()) {
			t.Error(ExpectedActual(uint32(vk.Height()), ack.Height))
		}
		// attach services to the leaf and register them with the VK
		for range serviceCount {
			serviceName := randomdata.SillyName()
			leaves[i].services[serviceName] = RandomLocalhostAddrPort()
			parentID, accept, err := client.Register(t.Context(), leaves[i].id, vk.Address(), serviceName, leaves[i].services[serviceName], 3*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if parentID != vk.ID() {
				t.Error(ExpectedActual(vk.ID(), parentID))
			} else if serviceName != accept.Service {
				t.Error(ExpectedActual(accept.Service, serviceName))
			}
			t.Logf("attached service '%s' @ %v to leaf %v", serviceName, leaves[i].services[serviceName], leaves[i].id)
		}

		cancel, err := client.AutoServiceHeartbeat(leafHBFreq*2, leafHBFreq, leaves[i].id, client.ParentInfo{ID: vk.ID(), Addr: vk.Address()}, slices.Collect(maps.Keys(leaves[i].services)), hbErrs[i])
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cancel <- true
		}()
	}
	t.Logf("%d leaves established", len(leaves))
	// wait for a brief period to see if leaf heartbeats fail
	// we cannot cleanly wait on a slice of channels (without reflection) so wait on them in a goroutine
	var (
		atErr atomic.Value
		wg    sync.WaitGroup
	)
	for _, hbe := range hbErrs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case e := <-hbe:
				atErr.CompareAndSwap(nil, e) // attempt to swap; only the first error will be preserved
			case <-time.After(leafHBFreq * 4):
			}
		}()
	}
	t.Log("waiting....")
	wg.Wait()
	t.Log("heartbeat errors")

	// check for an error
	if err := atErr.Load(); err != nil {
		t.Fatal("heartbeat error: ", err)
	}

	// now that our leaves are running and heartbeating, add "ssh" to a couple leaves to ensure we have multiple providers
	/*sshOwner1 := rand.UintN(uint(len(leaves)))
	sshOwner2 := rand.UintN(uint(len(leaves)))
	for sshOwner1 == sshOwner2 {
		sshOwner2 = rand.UintN(uint(len(leaves)))
	}*/

}

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
	allGoodChildrenValid(t, parent.Snapshot(), goodCVKs)
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
	time.Sleep(parentCVKPruneTime + 5*time.Millisecond)
	// confirm that bad cvk is no longer considered a child
	{
		if _, found := parent.Snapshot().Children.CVKs[badCVK.ID()]; found {
			t.Errorf("bad child (ID%d) has not been pruned", badCVK.ID())
		}
	}
	// check that the parent still recognizes all good cvks
	allGoodChildrenValid(t, parent.Snapshot(), goodCVKs)
}

// check that the parent recognizes all good cvks
func allGoodChildrenValid(t *testing.T, snapshot vaultkeeper.VKSnapshot, goodCVKs []*vaultkeeper.VaultKeeper) {
	t.Helper()
	for i, gcvk := range goodCVKs {
		if v, found := snapshot.Children.CVKs[gcvk.ID()]; !found {
			t.Errorf("failed to find child #%d (ID%d)", i, gcvk.ID())
		} else if len(v.Services) != 0 {
			t.Errorf("expected no services to be registered to child #%d (ID%d). Found: %v", i, gcvk.ID(), v.Services)
		} else if v.Addr != goodCVKs[i].Address() {
			t.Error("bad cvk address", ExpectedActual(goodCVKs[i].Address(), v.Addr))
		}
	}
}

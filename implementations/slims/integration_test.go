package slims_test

// This file provides external testing to the slims library.
// Lots of overlap with individual unit tests, making this somewhat redundant.

import (
	"maps"
	"math/rand/v2"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
)

// HeaderSerialization tests are serviced by implementations/slims/slims/protocol/protocol_test.go (and most thoroughly by TestFullSend()).

// EndpointArgs tests are serviced.... all over the place.
// This implementation does not use endpoints; instead, we switch on packet types. See basically all the tests in implementations/slims/slims/client/client_test.go and all of the Test_serve* tests in  implementations/slims/slims/vk/vaultkeeper_test.go.

// StatusRequest is services via TestVaultKeeper_StartStop in implementations/slims/slims/vk/vaultkeeper_test.go and TestStatus in implementations/slims/slims/client/client_test.go.

// TestMultiServiceMultiLeaf spins up a single vk and joins multiple leaves under it.
// Each leaf offers at least one service and at least one service is offered by multiple leaves.
func TestMultiServiceMultiLeaf(t *testing.T) {
	const (
		maxLeaves          uint32        = 25
		maxServicesPerLeaf uint32        = 3
		staleTime          time.Duration = 10 * time.Minute // disable service stale times

		leafHBFreq time.Duration = 100 * time.Millisecond // how often the leaves actually heartbeat
	)
	var pt = vaultkeeper.PruneTimes{
		Hello:           10 * time.Second,       // irrelevant for this test
		ServicelessLeaf: 300 * time.Millisecond, // to prove that heartbeats actually work
		ChildVK:         10 * time.Second,       // irrelevant for this test
	}

	vk, err := vaultkeeper.New(UniqueID(), RandomLocalhostAddrPort(),
		vaultkeeper.WithPruneTimes(pt), vaultkeeper.WithLogger(nil))
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	t.Logf("vk %d established @ %v", vk.ID(), vk.Address())
	// spawn a bunch of (at least 2) leaves and a channel to catch heartbeat errors from them.
	var (
		leaves = make([]Leaf, rand.Uint32N(maxLeaves)+2)
		hb     = make(chan error, 10)
	)

	for i := range leaves {
		// spawn at least 1 service per leaf
		serviceCount := rand.Uint32N(maxServicesPerLeaf) + 1
		leaves[i] = Leaf{
			ID: UniqueID(),
			Services: make(map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}, serviceCount),
		}
		for range serviceCount {
			leaves[i].Services[randomdata.Alphanumeric(48)] = struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{staleTime, RandomLocalhostAddrPort()}
		}
		expectedRegistered := slices.Collect(maps.Keys(leaves[i].Services))

		// attach it to the vk
		if actuallyRegistered, err := client.RegisterNewLeaf(t.Context(), leaves[i].ID, vk.Address(), leaves[i].Services); err != nil {
			t.Fatal(err)
		} else if !SlicesUnorderedEqual(actuallyRegistered, expectedRegistered) {
			t.Fatal("services registered does not match services associated to a leaf", ExpectedActual(expectedRegistered, actuallyRegistered))
		}

		cancel, err := client.AutoServiceHeartbeat(leafHBFreq*2, leafHBFreq, leaves[i].ID, client.ParentInfo{ID: vk.ID(), Addr: vk.Address()}, slices.Collect(maps.Keys(leaves[i].Services)), hb)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cancel()
		}()
	}
	t.Logf("%d leaves established", len(leaves))
	// wait for a brief period to see if leaf heartbeats fail
	checkAutoHBErrs(t, hb, leafHBFreq*3)

	// now that our leaves are running and heartbeating, add "ssh" to a couple leaves to ensure we have multiple providers
	sharedService := "ssh"
	sshOwnerIdx1 := rand.UintN(uint(len(leaves)))
	sshOwnerIdx2 := rand.UintN(uint(len(leaves)))
	for sshOwnerIdx1 == sshOwnerIdx2 {
		sshOwnerIdx2 = rand.UintN(uint(len(leaves)))
	}
	leaves[sshOwnerIdx1].Services["ssh"] = struct {
		Stale time.Duration
		Addr  netip.AddrPort
	}{10 * time.Second, RandomLocalhostAddrPort()}
	if _, _, err := client.Register(t.Context(), leaves[sshOwnerIdx1].ID, vk.Address(), sharedService, leaves[sshOwnerIdx1].Services[sharedService].Addr, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	cancel1, err := client.AutoServiceHeartbeat(leafHBFreq*2, leafHBFreq, leaves[sshOwnerIdx1].ID, client.ParentInfo{ID: vk.ID(), Addr: vk.Address()}, slices.Collect(maps.Keys(leaves[sshOwnerIdx1].Services)), hb)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel1()
	}()
	leaves[sshOwnerIdx2].Services["ssh"] = struct {
		Stale time.Duration
		Addr  netip.AddrPort
	}{10 * time.Second, RandomLocalhostAddrPort()}
	if _, _, err := client.Register(t.Context(), leaves[sshOwnerIdx2].ID, vk.Address(), sharedService, leaves[sshOwnerIdx2].Services[sharedService].Addr, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	cancel2, err := client.AutoServiceHeartbeat(leafHBFreq*2, leafHBFreq, leaves[sshOwnerIdx2].ID, client.ParentInfo{ID: vk.ID(), Addr: vk.Address()}, slices.Collect(maps.Keys(leaves[sshOwnerIdx2].Services)), hb)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel2()
	}()
	checkAutoHBErrs(t, hb, leafHBFreq*4)

	// check that the vk recognizes all of its children
	{
		snap := vk.Snapshot()
		// collect all the services each leaf owns
		expectedLeafServices := map[string]uint32{} // service name -> provider count
		for _, l := range leaves {
			for service := range l.Services {
				expectedLeafServices[service] += 1
			}
		}
		actualLeafServices := map[string]uint32{}
		for _, l := range snap.Children.Leaves {
			for service := range l {
				actualLeafServices[service] += 1
			}
		}
		if !maps.Equal(expectedLeafServices, actualLeafServices) {
			t.Fatal("status reported different services and provider counts",
				ExpectedActual(expectedLeafServices, actualLeafServices))
		}
	}

	// make a list request
	// TODO

	// make a get request
	// TODO
}

// waits on the given channel for the given time, calling t.Error() if anything arrives over the channel.
func checkAutoHBErrs(t *testing.T, ch chan error, wait time.Duration) {
	select {
	case e := <-ch:
		t.Error(e)
		// drain a handful of errors and die
		eCount := len(ch)
		for range eCount {
			t.Error(<-ch)
		}
		t.FailNow()
	case <-time.After(wait):
		t.Log("no initial heartbeat errors found. Continuing...")
	}
}

// TestTwoLayerVault spins up a root vk and joins several children under it, then ensures only the ones with no auto-beater get pruned.
func TestTwoLayerVault(t *testing.T) {
	const (
		parentCVKPruneTime = 100 * time.Millisecond
		childHeartbeatFreq = 20 * time.Millisecond
		badHBLimit         = 2
	)
	parent, err := vaultkeeper.New(UniqueID(), RandomLocalhostAddrPort(),
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
		goodCVKs[i], err = vaultkeeper.New(UniqueID(), RandomLocalhostAddrPort(),
			vaultkeeper.WithCustomHeartbeats(true, childHeartbeatFreq, badHBLimit))
		if err != nil {
			t.Fatal(err)
		} else if err := goodCVKs[i].Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(goodCVKs[i].Stop)
		wg.Add(1)
		go func(cvk *vaultkeeper.VaultKeeper) {
			defer wg.Done()
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
	badCVK, err := vaultkeeper.New(UniqueID(), RandomLocalhostAddrPort(),
		vaultkeeper.WithCustomHeartbeats(false, 0, 0))
	if err != nil {
		t.Fatal("failed to create bad (heartbeat-less) CVK: ", err)
	} else if err := badCVK.Start(); err != nil {
		t.Fatal("failed to start bad cvk", err)
	}
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

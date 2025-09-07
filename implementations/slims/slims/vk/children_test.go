package vaultkeeper

import (
	"math/rand/v2"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/rflandau/Orv/implementations/slims/internal/misc"
	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
)

func Test_addLeaf(t *testing.T) {
	vk, err := New(1, netip.MustParseAddrPort("127.0.0.1:"+strconv.FormatUint(uint64(misc.RandomPort()), 10)),
		WithPruneTimes(PruneTimes{ServicelessLeaf: 40 * time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}

	{ // add a new leaf
		leafID := rand.Uint64()
		if isCVK := vk.addLeaf(leafID); isCVK {
			t.Fatal("conflicted with existing cvk, despite being only child")
		}
		// ensure the leaf exists
		if leaf, found := vk.children.leaves[leafID]; !found {
			t.Error("failed to find leaf after insertion")
		} else if !leaf.servicelessPruner.Stop() {
			t.Error("serviceless pruner had already expired or been stopped")
		}
		// add the same leaf and check again
		if isCVK := vk.addLeaf(leafID); isCVK {
			t.Fatal("conflicted with existing cvk, despite being only child")
		}
		if leaf, found := vk.children.leaves[leafID]; !found {
			t.Error("failed to find leaf after insertion")
		} else if leaf.servicelessPruner.Stop() {
			t.Error("serviceless pruner had not been stopped already despite explicit stop call")
		}
	}

	{ // add another new leaf
		leafID := rand.Uint64()
		if isCVK := vk.addLeaf(leafID); isCVK {
			t.Fatal("conflicted with existing cvk, but no cvks should exist")
		}
		vk.children.mu.Lock()
		// ensure the leaf exists
		if _, found := vk.children.leaves[leafID]; !found {
			t.Error("failed to find leaf after insertion")
		}
		vk.children.mu.Unlock()
		// let the leaf expire
		time.Sleep(41 * time.Millisecond)
		// ensure the leaf does not exist
		vk.children.mu.Lock()
		if _, found := vk.children.leaves[leafID]; found {
			t.Error("leaf should have expired")
		}
		vk.children.mu.Unlock()
	}
	// add a cvk, then add a leaf with the same id
	{
		nodeID := rand.Uint64()
		if isLeaf := vk.addCVK(nodeID, netip.MustParseAddrPort("127.0.0.1:1")); isLeaf {
			t.Fatal("conflicted with existing leaf")
		}
		// ensure the cvk exists
		if _, found := vk.children.cvks.Load(nodeID); !found {
			t.Error("failed to find cvk after insertion")
		}
		// attempt to install a leaf with the same id
		if isCVK := vk.addLeaf(nodeID); !isCVK {
			t.Error("addLeaf should have recognized that a cvk with the same id already exists")
		}
		if _, found := vk.children.leaves[nodeID]; found {
			t.Error("leave was created despite conflicted cvk id")
		}
	}
}

func Test_addCVK(t *testing.T) {
	vk, err := New(1, netip.MustParseAddrPort("127.0.0.1:"+strconv.FormatUint(uint64(misc.RandomPort()), 10)),
		WithPruneTimes(PruneTimes{ServicelessLeaf: 40 * time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	// add a cvk
	cvkID := rand.Uint64()
	if isLeaf := vk.addCVK(cvkID, netip.MustParseAddrPort("[::0]:1234")); isLeaf {
		t.Fatal("conflicted with existing leaf, despite being only child")
	} else if _, found := vk.children.cvks.Load(cvkID); !found {
		t.Fatal("failed to find cvk after insertion")
	}
	{ // add a leaf and then try to add a conflicting cvk
		nodeID := rand.Uint64()
		if isCVK := vk.addLeaf(nodeID); isCVK {
			t.Fatal("conflicted with existing cvk")
		}
		if isLeaf := vk.addCVK(nodeID, netip.MustParseAddrPort("[::0]:1234")); !isLeaf {
			t.Fatal("expected to conflict with existing leaf")
		} else if _, found := vk.children.cvks.Load(nodeID); found {
			t.Fatal("cvk was inserted despite conflicting leaf id")
		}
	}
}

func Test_addService(t *testing.T) {
	t.Run("unknown child", func(t *testing.T) {
		vk, err := New(1, netip.MustParseAddrPort("[::0]:1234"))
		if err != nil {
			t.Fatal(err)
		}
		if err := vk.addService(1234, "ssh", netip.AddrPort{}, 1); err == nil {
			t.Fatal("expected error when given an ID for a child that DNE")
		}
	})
	t.Run("service to cvk", func(t *testing.T) {
		var (
			cvkID       = rand.Uint64()
			serviceName = randomdata.Currency()
			serviceAddr = netip.MustParseAddrPort("1.1.1.1:1")
		)
		vk, err := New(1, netip.MustParseAddrPort("[::0]:1234"),
			WithPruneTimes(PruneTimes{ChildVK: 50 * time.Millisecond}))
		if err != nil {
			t.Fatal(err)
		}
		// add a cvk
		if isLeaf := vk.addCVK(cvkID, netip.MustParseAddrPort("[::0]:4567")); isLeaf {
			t.Fatal("leaf should not exist on a fresh vk")
		}
		// add a service to the cvk
		if err := vk.addService(
			cvkID,
			serviceName,
			serviceAddr,
			0, // stale does not apply to cvk services
		); err != nil {
			t.Fatal(err)
		}
		vk.children.mu.Lock()
		// verify the service exists on the cvk
		if cvk, found := vk.children.cvks.Load(cvkID); !found {
			t.Fatal("failed to find just-added cvk")
		} else if cvk.services[serviceName] != serviceAddr {
			t.Fatal("incorrect service address", ExpectedActual(serviceAddr, cvk.services[serviceName]))
		}
		// verify that the service is listed with one provider
		if providers, found := vk.children.allServices[serviceName]; !found {
			t.Fatal("failed to find providers for newly-added service")
		} else if len(providers) != 1 {
			t.Fatal("incorrect provider count", ExpectedActual(1, len(providers)))
		} else if _, found := providers[cvkID]; !found {
			t.Fatalf("cvk is not a provider of the service: %#v", providers)
		}
		vk.children.mu.Unlock()
		// wait for the cvk to time out so we can ensure it is removed as a provider
		time.Sleep(52 * time.Millisecond)
		// check that the cvk is no longer found and not considered a provider
		vk.children.mu.Lock()
		if _, found := vk.children.cvks.Load(cvkID); found {
			t.Error("cvk should have been pruned out by this time")
		}
		if k, found := vk.children.allServices[serviceName]; found {
			if _, found := k[cvkID]; found {
				t.Error("cvk was not removed from the list of providers")
			}
		}
		vk.children.mu.Unlock()
	})
	t.Run("service to leaf plus expiry", func(t *testing.T) {
		var (
			leafID       = rand.Uint64()
			serviceName  = randomdata.Currency()
			serviceAddr  = netip.MustParseAddrPort("1.1.1.1:1")
			serviceStale = 30 * time.Millisecond
		)
		vk, err := New(1, netip.MustParseAddrPort("[::0]:1234"))
		if err != nil {
			t.Fatal(err)
		}
		// add a leaf
		if isCVK := vk.addLeaf(leafID); isCVK {
			t.Fatal("cvk should not exist on a fresh vk")
		}
		// add a service to the leaf
		if err := vk.addService(leafID, serviceName, serviceAddr, serviceStale); err != nil {
			t.Fatal(err)
		}
		vk.children.mu.Lock()
		// verify the service exists on the leaf
		if leaf, found := vk.children.leaves[leafID]; !found {
			t.Fatal("failed to find just-added leaf")
		} else if service, found := leaf.services[serviceName]; !found {
			t.Fatal("failed to find service registered to leaf")
		} else if service.addr != serviceAddr {
			t.Fatal("incorrect service address", ExpectedActual(serviceAddr, service.addr))
		} else if service.stale != serviceStale {
			t.Fatal("incorrect stale time", ExpectedActual(serviceStale, service.stale))
		}
		// verify the leaf is a provider of the service
		if addr, found := vk.children.allServices[serviceName][leafID]; !found {
			t.Fatal("leaf is not considered a provider of the service")
		} else if addr != serviceAddr {
			t.Fatal("bad service address in providers table", ExpectedActual(serviceAddr, addr))
		}
		vk.children.mu.Unlock()
		// wait for the service to get pruned from the leaf
		time.Sleep(serviceStale + 1*time.Millisecond)
		vk.children.mu.Lock()
		if leaf, found := vk.children.leaves[leafID]; !found {
			t.Fatal("leaf was pruned out")
		} else if _, found := leaf.services[serviceName]; found {
			t.Fatal("leaf service should have been pruned, but was found")
		} else if _, found := vk.children.allServices[serviceName][leafID]; found {
			t.Fatal("leaf is still listed as a provider of service in the providers table")
		}
		vk.children.mu.Unlock()
	})

}

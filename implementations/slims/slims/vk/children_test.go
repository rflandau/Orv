package vaultkeeper

import (
	"math/rand/v2"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/rflandau/Orv/implementations/slims/internal/misc"
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

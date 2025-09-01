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
	// add a new leaf
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
	// add another new leaf
	leafID = rand.Uint64()
	if isCVK := vk.addLeaf(leafID); isCVK {
		t.Fatal("conflicted with existing cvk, despite being only child")
	}
	// ensure the leaf exists
	if _, found := vk.children.leaves[leafID]; !found {
		t.Error("failed to find leaf after insertion")
	}
	// let the leaf expire
	time.Sleep(41 * time.Millisecond)
	// ensure the leaf does not exist
	if _, found := vk.children.leaves[leafID]; found {
		t.Error("leaf should have expired")
	}

}

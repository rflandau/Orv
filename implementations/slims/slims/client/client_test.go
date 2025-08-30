package client

import (
	"context"
	"math/rand/v2"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
	. "github.com/rflandau/Orv/internal/testsupport"
)

// Tests Status by spinning up a couple VKs and ensuring Status returns the correct information.
func TestStatus(t *testing.T) {
	t.Run("invalid IPAddr", func(t *testing.T) {
		ap, err := netip.ParseAddrPort("")
		if err == nil {
			t.Fatal("unexpected nil error")
		}
		if _, sr, err := Status(ap, nil); err == nil {
			t.Fatal("unexpected nil error")
		} else if sr != nil {
			t.Fatal("unexpected non-nil response")
		}
	})

	reqTimeout := 300 * time.Millisecond
	var vkIDA, vkIDB slims.NodeID = 1, rand.Uint64()

	// Spawn a VK to hit
	vkA, err := vaultkeeper.New(vkIDA, netip.MustParseAddrPort("[::0]:8081"))
	if err != nil {
		t.Fatal(err)
	}
	if err := vkA.Start(); err != nil {
		t.Fatal(err)
	}
	defer vkA.Stop()

	// submit a shorthand status request
	ctx, cancel := context.WithTimeout(t.Context(), reqTimeout)
	defer cancel()
	respVKID, respSR, err := Status(netip.MustParseAddrPort("[::0]:8081"), ctx)
	if err != nil {
		t.Fatal(err)
	} else if respSR == nil {
		t.Fatal("nil response")
	}
	// validate the response
	if vkA.Height() != uint16(respSR.Height) || respSR.Height != 0 {
		t.Error("bad height", ExpectedActual(0, respSR.Height))
	}
	if respVKID != vkIDA || vkA.ID() != vkIDA {
		t.Error("bad vkID", ExpectedActual(vkA.ID(), respVKID))
	}
	actualVersions := protocol.VersionsSupportedAsBytes()
	if slices.Compare(actualVersions, respSR.VersionsSupported) != 0 {
		t.Error("mismatching version list", ExpectedActual(respSR.VersionsSupported, actualVersions))
	}

	// spawn a second VK with alternate values
	vkB, err := vaultkeeper.New(vkIDB, netip.MustParseAddrPort("[::0]:8082"), vaultkeeper.WithDragonsHoard(3))
	if err != nil {
		t.Fatal(err)
	}
	if err := vkB.Start(); err != nil {
		t.Fatal(err)
	}
	defer vkB.Stop()

	// submit a longform status request
	ctxB, cancelB := context.WithTimeout(t.Context(), reqTimeout)
	defer cancelB()
	respVKID, respSR, err = Status(netip.MustParseAddrPort("[::0]:8082"), ctxB, 100)
	if err != nil {
		t.Fatal(err)
	}

	if vkB.Height() != uint16(respSR.Height) || respSR.Height != 3 {
		t.Error("bad height", ExpectedActual(3, respSR.Height))
	}
	if expectedRespID := vkB.ID(); respVKID != expectedRespID || vkIDB != expectedRespID {
		t.Error("bad vkID", ExpectedActual(vkB.ID(), respVKID))
	}
	if slices.Compare(actualVersions, respSR.VersionsSupported) != 0 {
		t.Error("mismatching version list", ExpectedActual(respSR.VersionsSupported, actualVersions))
	}
}

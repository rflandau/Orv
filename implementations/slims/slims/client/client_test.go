package client

import (
	"context"
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
	const reqTimeout = 300 * time.Millisecond
	actualVersions := protocol.VersionsSupportedAsBytes()

	t.Run("shorthand request", func(t *testing.T) {
		var (
			vkID   slims.NodeID = 1
			vkAddr              = netip.MustParseAddrPort("[::0]:8081")
		)

		// Spawn a VK to hit
		vkA, err := vaultkeeper.New(vkID, vkAddr)
		if err != nil {
			t.Fatal(err)
		}
		if err := vkA.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(vkA.Stop)

		// submit a shorthand status request
		ctx, cancel := context.WithTimeout(t.Context(), reqTimeout)
		defer cancel()
		respVKID, respSR, err := Status(vkAddr, ctx)
		if err != nil {
			t.Fatal(err)
		} else if respSR == nil {
			t.Fatal("nil response")
		}
		// validate the response
		if vkA.Height() != uint16(respSR.Height) || respSR.Height != 0 {
			t.Error("bad height", ExpectedActual(0, respSR.Height))
		}
		if respVKID != vkID || vkA.ID() != vkID {
			t.Error("bad vkID", ExpectedActual(vkA.ID(), respVKID))
		}
		if slices.Compare(actualVersions, respSR.VersionsSupported) != 0 {
			t.Error("mismatching version list", ExpectedActual(respSR.VersionsSupported, actualVersions))
		}
	})
	t.Run("longform request", func(t *testing.T) {
		var (
			vkID   slims.NodeID = 1
			vkAddr              = netip.MustParseAddrPort("[::0]:8082")
		)
		vkB, err := vaultkeeper.New(vkID, vkAddr, vaultkeeper.WithDragonsHoard(3))
		if err != nil {
			t.Fatal(err)
		}
		if err := vkB.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(vkB.Stop)

		// submit a longform status request
		ctxB, cancelB := context.WithTimeout(t.Context(), reqTimeout)
		defer cancelB()
		respVKID, respSR, err := Status(netip.MustParseAddrPort("[::0]:8082"), ctxB, 100)
		if err != nil {
			t.Fatal(err)
		}

		if vkB.Height() != uint16(respSR.Height) || respSR.Height != 3 {
			t.Error("bad height", ExpectedActual(3, respSR.Height))
		}
		if expectedRespID := vkB.ID(); respVKID != expectedRespID || vkID != expectedRespID {
			t.Error("bad vkID", ExpectedActual(vkB.ID(), respVKID))
		}
		if slices.Compare(actualVersions, respSR.VersionsSupported) != 0 {
			t.Error("mismatching version list", ExpectedActual(respSR.VersionsSupported, actualVersions))
		}
	})
	// spawn a second VK with alternate values

}

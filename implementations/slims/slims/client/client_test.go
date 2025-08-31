package client

import (
	"context"
	"math"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strconv"
	"sync"
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
	actualVersions := protocol.SupportedVersions()

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
		if slices.Compare(actualVersions.AsBytes(), respSR.VersionsSupported) != 0 {
			t.Error("mismatching version list", ExpectedActual(respSR.VersionsSupported, actualVersions.AsBytes()))
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
		if slices.Compare(actualVersions.AsBytes(), respSR.VersionsSupported) != 0 {
			t.Error("mismatching version list", ExpectedActual(respSR.VersionsSupported, actualVersions.AsBytes()))
		}
	})
}

// Tests that we can get HELLOs with valid output.
// Companion to vaultkeeper's Test_serveHello()
func TestHello(t *testing.T) {
	var (
		vkid, nodeID = rand.Uint64(), rand.Uint64()
		port         = uint16(rand.UintN(math.MaxUint16))
		ap           = netip.MustParseAddrPort("127.0.0.1:" + strconv.FormatUint(uint64(port), 10))
		repeat       = 5 // number of HELLOs to send
	)
	// spawn a VK
	vk, err := vaultkeeper.New(vkid, ap, vaultkeeper.WithDragonsHoard(2))
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	// generate node IDs
	nodeIDs := make([]slims.NodeID, repeat)
	var wg sync.WaitGroup
	for i := range repeat {
		nodeIDs[i] = rand.Uint64() // while it is theoretically possible for these to overlap, its incredibly unlikely so ¯\_(ツ)_/¯
		wg.Add(1)
		go func(nID slims.NodeID) { // kick off a hello for each
			defer wg.Done()
			// send a Hello
			respVKID, respVersion, respBody, err := Hello(context.Background(), nodeID, ap)
			if err != nil {
				panic(err)
			}
			// validate response
			if respVKID != vkid {
				panic(ExpectedActual(vkid, respVKID))
			} else if respBody.Height != 2 {
				panic(ExpectedActual(2, respBody.Height))
			} else if respVersion != protocol.SupportedVersions().HighestSupported() {
				panic(ExpectedActual(protocol.SupportedVersions().HighestSupported(), respVersion))
			}
		}(nodeIDs[i])
	}
	wg.Wait()

}

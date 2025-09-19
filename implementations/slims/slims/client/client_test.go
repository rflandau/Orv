package client_test

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

	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
)

// Tests Status by spinning up a couple VKs and ensuring Status returns the correct information.
func TestStatus(t *testing.T) {
	t.Run("invalid IPAddr", func(t *testing.T) {
		ap, err := netip.ParseAddrPort("")
		if err == nil {
			t.Fatal("unexpected nil error")
		}
		if _, sr, err := client.Status(ap, nil); err == nil {
			t.Fatal("unexpected nil error")
		} else if sr != nil {
			t.Fatal("unexpected non-nil response")
		}
	})
	const reqTimeout = 300 * time.Millisecond
	actualVersions := protocol.SupportedVersions()

	t.Run("shorthand request", func(t *testing.T) {
		var (
			vkID slims.NodeID = 1
		)

		// Spawn a VK to hit
		vkA, err := vaultkeeper.New(vkID, RandomLocalhostAddrPort())
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
		respVKID, respSR, err := client.Status(vkA.Address(), ctx)
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
			vkID slims.NodeID = 1
		)
		vkB, err := vaultkeeper.New(vkID, RandomLocalhostAddrPort(), vaultkeeper.WithDragonsHoard(3))
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
		respVKID, respSR, err := client.Status(vkB.Address(), ctxB, 100)
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
		ap           = RandomLocalhostAddrPort()
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
			respVKID, respVersion, respBody, err := client.Hello(context.Background(), nodeID, ap)
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

// NOTE(rlandau): does not test vk-joins; those are tested in the vaultkeeper package.
func TestJoin(t *testing.T) {
	var (
		nodeID       = rand.Uint64()
		VKAP         = RandomLocalhostAddrPort()
		repeat uint8 = 3 // for tests that run multiple times, the # of times to run
	)
	// spawn a VK
	vk, err := vaultkeeper.New(rand.Uint64(), VKAP)
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	defer vk.Stop()

	t.Run("premature join", func(t *testing.T) {
		for range repeat {
			// send a join
			vkID, accept, err := client.Join(t.Context(), nodeID, VKAP, client.JoinInfo{false, netip.AddrPort{}, 0})
			if err == nil {
				t.Fatal("expected a Join error due to premature join")
			} else if accept != nil {
				t.Fatalf("expected accept to be nil due to failure. Found %#v instead", accept)
			}
			if vkID != vk.ID() {
				t.Fatal(ExpectedActual(vk.ID(), vkID))
			}
		}
	})
	t.Run("join after mismatch hello", func(t *testing.T) {
		for range repeat {
			// send a hello, but for a different ID
			if vkID, _, ack, err := client.Hello(t.Context(), nodeID-uint64(rand.Uint()), VKAP); err != nil {
				t.Fatal(err)
			} else if vkID != vk.ID() {
				t.Fatal(ExpectedActual(vk.ID(), vkID))
			} else if ack == nil {
				t.Fatal("nil response body on successful hello")
			} else if ack.Height != uint32(vk.Height()) {
				t.Fatal(ExpectedActual(uint32(vk.Height()), ack.Height))
			}
			// send a join
			vkID, accept, err := client.Join(t.Context(), nodeID, VKAP, client.JoinInfo{false, netip.AddrPort{}, 0})
			if err == nil {
				t.Fatal("expected a Join error due to premature join")
			} else if accept != nil {
				t.Fatalf("expected accept to be nil due to failure. Found %#v instead", accept)
			}
			if vkID != vk.ID() {
				t.Fatal(ExpectedActual(vk.ID(), vkID))
			}
		}
	})
	t.Run("join and repeat", func(t *testing.T) {
		for i := range repeat {
			t.Run(strconv.FormatInt(int64(i), 10), func(t *testing.T) {
				// send a hello
				if vkID, _, ack, err := client.Hello(t.Context(), nodeID, VKAP); err != nil {
					t.Fatal(err)
				} else if vkID != vk.ID() {
					t.Fatal(ExpectedActual(vk.ID(), vkID))
				} else if ack == nil {
					t.Fatal("nil response body on successful hello")
				} else if ack.Height != uint32(vk.Height()) {
					t.Fatal(ExpectedActual(uint32(vk.Height()), ack.Height))
				}
				// send a join
				vkID, accept, err := client.Join(t.Context(), nodeID, VKAP, client.JoinInfo{false, netip.AddrPort{}, 0})
				// validate
				if err != nil {
					t.Fatal(err)
				} else if accept.Height != uint32(vk.Height()) {
					t.Fatal(ExpectedActual(uint32(vk.Height()), accept.Height))
				}
				if vkID != vk.ID() {
					t.Fatal(ExpectedActual(vk.ID(), vkID))
				}

			})
		}
	})
	// tests that join fails if too much time passes after a successfully HELLO.
	// !spawns a new VK, rather than using the parent tests's vk.
	t.Run("join after hello expires", func(t *testing.T) {
		nodeID := rand.Uint64N(math.MaxUint16)
		vkB, err := vaultkeeper.New(
			rand.Uint64(),
			RandomLocalhostAddrPort(),
			vaultkeeper.WithPruneTimes(vaultkeeper.PruneTimes{Hello: 30 * time.Millisecond}),
		)
		if err != nil {
			t.Fatal(err)
		} else if err := vkB.Start(); err != nil {
			t.Fatal(err)
		}
		defer vkB.Stop()
		t.Log("VKID: ", vkB.ID())
		t.Log("NodeID: ", nodeID)
		// send a hello
		vkIDHello, _, ack, err := client.Hello(t.Context(), nodeID, vkB.Address())
		if err != nil {
			t.Fatal(err)
		} else if vkIDHello != vkB.ID() {
			t.Fatal(ExpectedActual(vkB.ID(), vkIDHello))
		} else if ack.Height != uint32(vkB.Height()) {
			t.Fatal(uint32(vkB.Height()), ack.Height)
		}
		t.Log("VKID: ", vkB.ID())
		t.Log("NodeID: ", nodeID)
		// wait for that hello to expire
		time.Sleep(31 * time.Millisecond)
		// try to join
		if vkIDJoin, accept, err := client.Join(t.Context(), nodeID, vkB.Address(), client.JoinInfo{false, netip.AddrPort{}, 0}); err == nil {
			t.Fatal("expected a Join error due hello expiration")
		} else if accept != nil {
			t.Fatalf("expected accept to be nil due to failure. Found %#v instead", accept)
		} else if vkIDJoin != vkIDHello {
			t.Fatal(ExpectedActual(vkIDHello, vkIDJoin))
		} else if vkIDJoin != vkB.ID() {
			t.Fatal(ExpectedActual(vkB.ID(), vkIDJoin))
		}
	})

}

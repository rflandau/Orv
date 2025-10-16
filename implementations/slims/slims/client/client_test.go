package client_test

import (
	"context"
	"errors"
	"maps"
	"math"
	"math/rand/v2"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
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

func TestList(t *testing.T) {
	t.Run("invalid IPAddr", func(t *testing.T) {
		ap, err := netip.ParseAddrPort("")
		if err == nil {
			t.Fatal("unexpected nil error")
		}
		if _, services, err := client.List(ap, t.Context(), randomdata.Address(), 1, nil, 0); !errors.Is(err, client.ErrInvalidAddrPort) {
			t.Fatal("unexpected error", ExpectedActual[error](client.ErrInvalidAddrPort, err))
		} else if services != nil {
			t.Fatal("unexpected non-nil response")
		}
	})

	// spawn a vk and register some services
	const (
		pruneTO   time.Duration = 30 * time.Second
		staleTime time.Duration = 30 * time.Second // stale time to set for each service
	)
	var (
		cVK, parentVK       *vaultkeeper.VaultKeeper
		cVKLeaf, parentLeaf = Leaf{
			ID: rand.Uint64(),
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				randomdata.SillyName(): {staleTime, RandomLocalhostAddrPort()},
				randomdata.Month():     {staleTime, RandomLocalhostAddrPort()},
			},
		}, Leaf{
			ID: rand.Uint64(),
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				randomdata.Alphanumeric(5): {staleTime, RandomLocalhostAddrPort()},
				randomdata.Adjective():     {staleTime, RandomLocalhostAddrPort()},
			},
		}
	)
	{
		// spawn the child VK and associate services to it
		cVK = spawnVK(t, rand.Uint64(), cVKLeaf, vaultkeeper.WithPruneTimes(vaultkeeper.PruneTimes{Hello: pruneTO, ServicelessLeaf: pruneTO, ChildVK: pruneTO}), // disable pruning
			vaultkeeper.WithLogger(nil)) // disable logging
		t.Cleanup(cVK.Stop)
		// spawn the parent VK and associate services to it
		parentVK = spawnVK(t, rand.Uint64(), parentLeaf,
			vaultkeeper.WithPruneTimes(vaultkeeper.PruneTimes{Hello: pruneTO, ServicelessLeaf: pruneTO, ChildVK: pruneTO}),
			vaultkeeper.WithLogger(nil),
			vaultkeeper.WithDragonsHoard(1),
		)
		t.Cleanup(parentVK.Stop)

		// associate the childVK to the parent
		if _, _, _, err := client.Hello(t.Context(), cVK.ID(), parentVK.Address()); err != nil {
			t.Fatal(err)
		}
		if err := cVK.Join(t.Context(), parentVK.Address()); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("ParentVK: %v @ %v", parentVK.ID(), parentVK.Address())
	t.Logf("Leaf under Parent: %v", parentLeaf.ID)
	for service := range parentLeaf.Services {
		t.Log("---", service)
	}
	t.Logf("ChildVK: %v @ %v", cVK.ID(), cVK.Address())
	t.Logf("Leaf under ChildVK: %v", cVKLeaf.ID)
	for service := range cVKLeaf.Services {
		t.Log("---", service)
	}

	// sends requests against the child vk first
	var tests = []struct {
		name              string
		clientID          slims.NodeID // if 0, will be omitted from parameter list
		hopCount          uint16
		token             string
		expectedError     bool
		expectedServices  []string // only checked if expectedError is passed
		expectedResponder netip.AddrPort
	}{
		{name: "no token error", hopCount: 0, token: "", expectedError: true, expectedServices: nil, expectedResponder: cVK.Address()},
		{name: "shorthand request", hopCount: 0, token: randomdata.Address(), expectedError: false, expectedServices: slices.Collect(maps.Keys(cVKLeaf.Services)), expectedResponder: cVK.Address()},
		{name: "longform request", clientID: rand.Uint64(), hopCount: 0, token: randomdata.Address(), expectedError: false, expectedServices: slices.Collect(maps.Keys(cVKLeaf.Services)), expectedResponder: cVK.Address()},
		{name: "forward to parent", clientID: rand.Uint64(), hopCount: 2, token: randomdata.Address(), expectedError: false, expectedServices: append(slices.Collect(maps.Keys(cVKLeaf.Services)), slices.Collect(maps.Keys(parentLeaf.Services))...), expectedResponder: parentVK.Address()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//ctx, cancel := context.WithTimeout(t.Context(), reqTimeout)
			//defer cancel()
			var (
				responderAddr net.Addr
				services      []string
				err           error
			)
			t.Logf("(%v)token: %v", tt.name, tt.token)
			if tt.clientID != 0 {
				responderAddr, services, err = client.List(cVK.Address(), t.Context(), tt.token, tt.hopCount, nil, tt.clientID)
			} else {
				responderAddr, services, err = client.List(cVK.Address(), t.Context(), tt.token, tt.hopCount, nil)
			}
			if tt.expectedError && err == nil {
				t.Fatal("error was expected but was not returned")
			} else if !tt.expectedError {
				if err != nil {
					t.Fatal(err)
				}
				if services == nil {
					t.Fatal("no services found")
				} else if responderAddr.String() != tt.expectedResponder.String() { // ensure the original vk was also the responder
					t.Fatal("bad responder address", ExpectedActual(cVK.Address().String(), responderAddr.String()))
				}
				if !SlicesUnorderedEqual(tt.expectedServices, services) {
					t.Fatal("listed services do not match all registered services", ExpectedActual(tt.expectedServices, services))
				}
			}
		})
	}
}

func TestGet(t *testing.T) {
	serviceStale := 10 * time.Second
	t.Run("Simple", func(t *testing.T) { // Get request direct to VK with some leaf and two services
		l := Leaf{ID: rand.Uint64(), Services: map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}{"ssh": {Stale: serviceStale, Addr: RandomLocalhostAddrPort()}},
		}
		vk := spawnVK(t, rand.Uint64(), l)
		defer vk.Stop()
		if respAddr, svcAddr, err := client.Get(t.Context(), "ssh", vk.Address(), randomdata.Month(), 1, nil); err != nil {
			t.Fatal(err)
		} else if respAddr.String() != vk.Address().String() {
			t.Fatal("bad responder address", ExpectedActual(vk.Address().String(), respAddr.String()))
		} else if svcAddr != l.Services["ssh"].Addr.String() {
			t.Fatal("bad service address", ExpectedActual(l.Services["ssh"].Addr.String(), svcAddr))
		}
		// try again with an ID included, should be the same result
		if respAddr, svcAddr, err := client.Get(t.Context(), "ssh", vk.Address(), randomdata.Month(), 1, nil, rand.Uint64()); err != nil {
			t.Fatal(err)
		} else if respAddr.String() != vk.Address().String() {
			t.Fatal("bad responder address", ExpectedActual(vk.Address().String(), respAddr.String()))
		} else if svcAddr != l.Services["ssh"].Addr.String() {
			t.Fatal("bad service address", ExpectedActual(l.Services["ssh"].Addr.String(), svcAddr))
		}
		// try again (shorthand) with an unknown service
		if respAddr, svcAddr, err := client.Get(t.Context(), "unknown service", vk.Address(), randomdata.Month(), 1, nil); err != nil {
			t.Fatal(err)
		} else if respAddr.String() != vk.Address().String() {
			t.Fatal("bad responder address", ExpectedActual(vk.Address().String(), respAddr.String()))
		} else if svcAddr != "" {
			t.Fatal("bad service address", ExpectedActual("", svcAddr))
		}
	})
	t.Run("4-node linear topology", func(t *testing.T) { // GET requests propagate up a 4-node, linear "tree"
		// for ease-of-use: ID == height
		l0 := Leaf{
			ID: rand.Uint64(),
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{},
		}
		vk0 := spawnVK(t, 0, l0)
		t.Cleanup(vk0.Stop)
		l1 := Leaf{
			ID: rand.Uint64(),
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				"leaf1s1": {serviceStale, RandomLocalhostAddrPort()},
				"leaf1s2": {serviceStale, RandomLocalhostAddrPort()},
			},
		}
		vk1 := spawnVK(t, 1, l1, vaultkeeper.WithDragonsHoard(1))
		t.Cleanup(vk1.Stop)
		l2 := Leaf{
			ID: rand.Uint64(),
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				"leaf2s1": {serviceStale, RandomLocalhostAddrPort()},
			},
		}
		vk2 := spawnVK(t, 2, l2, vaultkeeper.WithDragonsHoard(2))
		t.Cleanup(vk2.Stop)
		l3 := Leaf{
			ID: rand.Uint64(),
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				"leaf3s1": {serviceStale, RandomLocalhostAddrPort()},
				"leaf3s2": {serviceStale, RandomLocalhostAddrPort()},
			},
		}
		vk3 := spawnVK(t, 3, l3, vaultkeeper.WithDragonsHoard(3))
		t.Cleanup(vk3.Stop)
		// join all the VKs together
		{
			// vk0 -> vk1
			if _, _, _, err := client.Hello(t.Context(), vk0.ID(), vk1.Address()); err != nil {
				t.Fatal(err)
			}
			if err := vk0.Join(t.Context(), vk1.Address()); err != nil {
				t.Fatal(err)
			}
			// vk0 -> vk1 -> vk2
			if _, _, _, err := client.Hello(t.Context(), vk1.ID(), vk2.Address()); err != nil {
				t.Fatal(err)
			}
			if err := vk1.Join(t.Context(), vk2.Address()); err != nil {
				t.Fatal(err)
			}
			// vk0 -> vk1 -> vk2 -> vk3
			if _, _, _, err := client.Hello(t.Context(), vk2.ID(), vk3.Address()); err != nil {
				t.Fatal(err)
			}
			if err := vk2.Join(t.Context(), vk3.Address()); err != nil {
				t.Fatal(err)
			}
		}
		// make requests
		var tests = []struct {
			name                  string
			requestedService      string
			targetVK              *vaultkeeper.VaultKeeper
			hopLimit              uint16
			expectedResponderAddr string
			expectedServiceAddr   string // what we expect the service's address to be ("" if not found)
		}{} // TODO
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// generate a token
				tkn := randomdata.Currency()

				respAddr, serviceAddr, err := client.Get(t.Context(), tt.requestedService, tt.targetVK.Address(), tkn, tt.hopLimit, nil)
				if err != nil {
					t.Fatal(err)
				} else if respAddr.String() != tt.expectedResponderAddr {
					t.Fatal("bad responder address", ExpectedActual(tt.expectedResponderAddr, respAddr.String()))
				} else if serviceAddr != tt.expectedServiceAddr {
					t.Fatal("bad service address", ExpectedActual(tt.expectedServiceAddr, serviceAddr))
				}
			})
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

// helper function to spin up a vk, start it, and register the given leaf (and its services) under it.
// Fatal on error.
// Remember to defer vk.Stop().
// Attaches a 500ms timeout to each operation.
func spawnVK(t *testing.T, childID slims.NodeID, childLeaf Leaf, vkOpts ...vaultkeeper.VKOption) *vaultkeeper.VaultKeeper {
	t.Helper()
	ctx, cnl := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cnl()

	vk, err := vaultkeeper.New(rand.Uint64(), RandomLocalhostAddrPort(), vkOpts...)
	if err != nil {
		t.Fatal(err)
	} else if err = vk.Start(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := client.Hello(ctx, childID, vk.Address()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Join(ctx, childID, vk.Address(), client.JoinInfo{IsVK: false}); err != nil {
		t.Fatal(err)
	}
	for service, info := range childLeaf.Services {
		if _, _, err := client.Register(ctx, childID, vk.Address(), service, info.Addr, info.Stale); err != nil {
			t.Fatal(err)
		}
	}

	return vk
}

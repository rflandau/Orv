package vaultkeeper

import (
	"errors"
	"math"
	"math/rand/v2"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/rflandau/Orv/implementations/slims/internal/misc"
	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"google.golang.org/protobuf/proto"
)

// Starts and stops a VK back to back, checking that we can successfully send/receive a STATUS message after each start and cannot do so after each stop
func TestVaultKeeper_StartStop(t *testing.T) {
	var (
		vkid   slims.NodeID = 1
		port                = rand.UintN(math.MaxUint16)
		vkAddr              = netip.MustParseAddrPort("127.0.0.1:" + strconv.FormatUint(uint64(port), 10))
	)
	vk, err := New(vkid, vkAddr)
	if err != nil {
		t.Fatal(err)
	}
	if vk.ID() != vkid {
		t.Error("incorrect ID from getter", ExpectedActual(vkid, vk.ID()))
	}
	if vk.Height() != 0 {
		t.Error("incorrect height from getter", ExpectedActual(vkid, vk.ID()))
	}

	startAndCheck(t, vk)
	stopAndCheck(t, vk)

	startAndCheck(t, vk)
	stopAndCheck(t, vk)

	startAndCheck(t, vk)
	stopAndCheck(t, vk)

	startAndCheck(t, vk)
	stopAndCheck(t, vk)
}

// helper function.
// Calls .Start() on the given VK, then ensures we can successfully hit it with a STATUS packet.
func startAndCheck(t *testing.T, vk *VaultKeeper) {
	if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	if alive, err := upstate(t, vk); !alive {
		t.Fatal("vk is dead after starting")
	} else if err != nil {
		t.Fatal(err)
	}
}

// helper function.
// Calls .Stop() on the given VK, then ensures that STATUS fails against it.
func stopAndCheck(t *testing.T, vk *VaultKeeper) {
	vk.Stop()
	if alive, err := upstate(t, vk); alive {
		t.Fatal("vk is alive after stopping")
	} else if err == nil {
		t.Fatal("expected an error trying to hit STATUS on a stopped vk")
	}
}

// upstate returns the alive state of the given vk and the result of trying to hit it with a STATUS packet (no matter its declared `alive` status).
func upstate(t *testing.T, vk *VaultKeeper) (alive bool, srErr error) {
	t.Helper()
	alive = vk.net.accepting.Load()

	// send a STATUS packet
	if respVKID, sr, err := client.Status(netip.MustParseAddrPort(vk.addr.String()), t.Context()); err != nil {
		return alive, err
	} else if sr == nil {
		return alive, errors.New("nil response")
	} else if respVKID != vk.id {
		return alive, errors.New("unexpected responder ID" + ExpectedActual(vk.id, respVKID))
	}

	return alive, nil
}

// Ensures that the data returned by respondError looks as we expect it to.
func Test_respondError(t *testing.T) {
	const rcvrAddr string = "127.0.0.1:8081"
	// spawn a listener to receive the FAULT
	ch := make(chan struct {
		n          int
		senderAddr net.Addr
		header     protocol.Header
		respBody   []byte
		err        error
	})
	{
		rcvr, err := net.ListenPacket("udp", rcvrAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer rcvr.Close()
		go func() {
			n, senderAddr, respHdr, respBody, err := protocol.ReceivePacket(rcvr, t.Context())
			ch <- struct {
				n          int
				senderAddr net.Addr
				header     protocol.Header
				respBody   []byte
				err        error
			}{n, senderAddr, respHdr, respBody, err}
		}()
	}
	const (
		reason string = "test"
	)
	var (
		vkAddr         = netip.MustParseAddrPort("127.0.0.1:8080")
		expectedHeader = protocol.Header{
			Version: version.Version{Major: 2},
			Type:    mt.Fault,
			ID:      1,
		}
	)
	// spin up the vk
	vk, err := New(1, vkAddr, WithVersions(version.NewSet(version.Version{Major: 2})))
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	defer vk.Stop()

	// send the fault packet
	parsedRcvrAddr, err := net.ResolveUDPAddr("udp", rcvrAddr)
	if err != nil {
		t.Fatal(err)
	}
	vk.respondError(parsedRcvrAddr, "test")

	res := <-ch
	// test that it looks as expected
	if res.err != nil {
		t.Fatal(err)
	}
	if sa := netip.MustParseAddrPort(res.senderAddr.String()); sa != vkAddr {
		t.Error(ExpectedActual(vkAddr, sa))
	}
	if res.header != expectedHeader {
		t.Error(ExpectedActual(expectedHeader, res.header))
	}
	bd := &pb.Fault{}
	if err := proto.Unmarshal(res.respBody, bd); err != nil {
		t.Fatal(err)
	}
	if bd.Reason != reason {
		t.Error(ExpectedActual(reason, bd.Reason))
	}
}

// Ensures that the data returned by respondSuccess looks as we expect it to.
func Test_respondSuccess(t *testing.T) {
	var (
		port     = rand.UintN(math.MaxUint16)
		rcvrAddr = "127.0.0.1:" + strconv.FormatInt(int64(port), 10)
	)
	// spawn a listener to receive the FAULT
	ch := make(chan struct {
		n          int
		senderAddr net.Addr
		header     protocol.Header
		respBody   []byte
		err        error
	})
	go func() {
		rcvr, err := net.ListenPacket("udp", rcvrAddr)
		if err != nil {
			ch <- struct {
				n          int
				senderAddr net.Addr
				header     protocol.Header
				respBody   []byte
				err        error
			}{0, nil, protocol.Header{}, nil, err}
		}
		defer rcvr.Close()
		n, senderAddr, respHdr, respBody, err := protocol.ReceivePacket(rcvr, t.Context())
		ch <- struct {
			n          int
			senderAddr net.Addr
			header     protocol.Header
			respBody   []byte
			err        error
		}{n, senderAddr, respHdr, respBody, err}
	}()

	var (
		vkAddr  = netip.MustParseAddrPort("127.0.0.1:8080")
		sentHdr = protocol.Header{
			Version: version.Version{Major: 1, Minor: 12},
			Type:    mt.HelloAck,
			ID:      1,
		}
		sentPayload = &pb.HelloAck{Height: 10}
	)
	// spin up the vk
	vk, err := New(1, vkAddr)
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	defer vk.Stop()

	// send the fault packet
	parsedRcvrAddr, err := net.ResolveUDPAddr("udp", rcvrAddr)
	if err != nil {
		t.Fatal(err)
	}
	vk.respondSuccess(parsedRcvrAddr, sentHdr, sentPayload)

	res := <-ch
	// test that it looks as expected
	if res.err != nil {
		t.Fatal(err)
	}
	if sa := netip.MustParseAddrPort(res.senderAddr.String()); sa != vkAddr {
		t.Error(ExpectedActual(vkAddr, sa))
	}
	if res.header != sentHdr {
		t.Error(ExpectedActual(sentHdr, res.header))
	}
	rcvdPayload := &pb.HelloAck{}
	if err := proto.Unmarshal(res.respBody, rcvdPayload); err != nil {
		t.Error(err)
	}
	if rcvdPayload.Height != sentPayload.Height {
		t.Error(ExpectedActual(sentPayload.Height, rcvdPayload.Height))
	}
}

// Spins up a vk, sends a hello, then checks the vk's pending hello table
func Test_serveHello(t *testing.T) {
	var (
		vkid = rand.Uint64()
		vkAP = netip.MustParseAddrPort("[::0]:" + strconv.FormatUint(uint64(misc.RandomPort()), 10))
	)
	// spin up a VK
	ddl, _ := t.Deadline()
	vk, err := New(vkid, vkAP,
		WithPruneTimes(PruneTimes{Hello: time.Until(ddl)}),
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	for i := range 4 { // run it multiple times to ensure no hangs
		t.Run(strconv.FormatInt(int64(i), 10), func(t *testing.T) {
			// send a hello to the vk
			clientID := rand.Uint64()
			respVKID, respVKVersion, ack, err := client.Hello(t.Context(), clientID, vkAP)
			if err != nil {
				t.Fatal(err)
			}
			if respVKID != vkid {
				t.Error(ExpectedActual(vkid, respVKID))
			}
			if respVKVersion != vk.versionSet.HighestSupported() {
				t.Error(ExpectedActual(vk.versionSet.HighestSupported(), respVKVersion))
			}
			if ack.Height != uint32(vk.Height()) {
				t.Error(ExpectedActual(vk.Height(), uint16(ack.Height)))
			}
			// check that our clientID is in the pendingHello table
			if _, found := vk.pendingHellos.Load(clientID); !found {
				t.Errorf("client ID %d is not in the vk's pending hellos", clientID)
			}
		})
	}
	t.Run("pending timeout", func(t *testing.T) {
		var (
			vkid    = rand.Uint64()
			port    = uint16(rand.Uint32N(math.MaxUint16))
			vkAP    = netip.MustParseAddrPort("[::0]:" + strconv.FormatUint(uint64(port), 10))
			timeout = 30 * time.Millisecond
		)
		// spin up a VK
		vk, err := New(vkid, vkAP, WithPruneTimes(PruneTimes{Hello: timeout}))
		if err != nil {
			t.Fatal(err)
		} else if err := vk.Start(); err != nil {
			t.Fatal(err)
		}
		// send a hello to the vk
		clientID := rand.Uint64()
		respVKID, respVKVersion, ack, err := client.Hello(t.Context(), clientID, vkAP)
		if err != nil {
			t.Fatal(err)
		}
		if respVKID != vkid {
			t.Error(ExpectedActual(vkid, respVKID))
		}
		if respVKVersion != vk.versionSet.HighestSupported() {
			t.Error(ExpectedActual(vk.versionSet.HighestSupported(), respVKVersion))
		}
		if ack.Height != uint32(vk.Height()) {
			t.Error(ExpectedActual(vk.Height(), uint16(ack.Height)))
		}
		time.Sleep(timeout + 3*time.Millisecond)
		// check that our clientID has expired
		if _, found := vk.pendingHellos.Load(clientID); found {
			t.Errorf("client ID %d is in the vk's pending hellos but should have timed out", clientID)
		}
	})
}

// Tests both the serveJoin handler and the vaultkeeper.Join request method.
// Companion test to the Join tests in the client package.
func Test_Join(t *testing.T) {
	// spin up a parent
	parentVK, err := New(
		rand.Uint64(),
		netip.MustParseAddrPort("127.0.0.1:"+strconv.FormatUint(uint64(misc.RandomPort()), 10)),
		WithDragonsHoard(1),
		WithPruneTimes(PruneTimes{Hello: 10 * time.Second}),
	)
	if err != nil {
		t.Fatal(err)
	} else if err := parentVK.Start(); err != nil {
		t.Fatal(err)
	}
	defer parentVK.Stop()

	{
		// spin up a child
		childVK, err := New(
			rand.Uint64(),
			netip.MustParseAddrPort("127.0.0.1:"+strconv.FormatUint(uint64(misc.RandomPort()), 10)),
		)
		if err != nil {
			t.Fatal(err)
		} else if err := childVK.Start(); err != nil {
			t.Fatal(err)
		}
		defer childVK.Stop()

		// join the child under the parent
		t.Logf("child (%d) sending HELLO to parent (%d)", childVK.ID(), parentVK.ID())
		if id, ver, ack, err := client.Hello(t.Context(), childVK.ID(), parentVK.Address()); err != nil {
			t.Fatal(err)
		} else if id != parentVK.ID() {
			t.Fatal(ExpectedActual(parentVK.ID(), id))
		} else if ver != parentVK.versionSet.HighestSupported() || ver != childVK.versionSet.HighestSupported() {
			t.Fatalf("versions mismatch.\n %v (response) != %v (parent) != %v (child)", ver, parentVK.versionSet.HighestSupported(), childVK.versionSet.HighestSupported())
		} else if ack.Height != uint32(parentVK.Height()) {
			t.Fatal(ExpectedActual(uint32(parentVK.Height()), ack.Height))
		}
		t.Logf("child (%d) sending JOIN to parent (%d)", childVK.ID(), parentVK.ID())
		if err := childVK.Join(t.Context(), parentVK.Address()); err != nil {
			t.Fatal(err)
		}
		// validate changes to the child
		if childVK.structure.parentID != parentVK.ID() {
			t.Error(ExpectedActual(parentVK.ID(), childVK.structure.parentID))
		}
		if childVK.structure.parentAddr != parentVK.Address() {
			t.Error(ExpectedActual(parentVK.Address(), childVK.structure.parentAddr))
		}
		// validate that the child is in the parent's children tables
		if _, found := parentVK.children.cvks.Load(childVK.ID()); !found {
			t.Error("did not find a child vk associated to childVK's ID")
		}
	}
	// join a leaf under parent
	{
		leafID := rand.Uint64()
		t.Logf("child (%d) sending HELLO to parent (%d)", leafID, parentVK.ID())
		if id, ver, ack, err := client.Hello(t.Context(), leafID, parentVK.Address()); err != nil {
			t.Fatal(err)
		} else if id != parentVK.ID() {
			t.Fatal(ExpectedActual(parentVK.ID(), id))
		} else if ver != parentVK.versionSet.HighestSupported() {
			t.Fatalf("versions mismatch.\n %v (response) != %v (parent)", ver, parentVK.versionSet.HighestSupported())
		} else if ack.Height != uint32(parentVK.Height()) {
			t.Fatal(ExpectedActual(uint32(parentVK.Height()), ack.Height))
		}
		t.Logf("child (%d) sending JOIN to parent (%d)", leafID, parentVK.ID())
		if vkID, accept, err := client.Join(t.Context(), leafID, parentVK.Address(), struct {
			IsVK   bool
			VKAddr netip.AddrPort
			Height uint16
		}{false, netip.AddrPort{}, 0}); err != nil {
			t.Fatal(err)
		} else if vkID != parentVK.ID() {
			t.Fatal(ExpectedActual(parentVK.ID(), vkID))
		} else if accept.Height != uint32(parentVK.Height()) {
			t.Fatal(uint32(parentVK.Height()), accept.Height)
		}
		// validate that the child is in the parent's children tables
		if _, found := parentVK.children.leaves[leafID]; !found {
			t.Error("did not find a child leaf associated to leaf's ID")
		}
	}
}

// Tests both the serveVKHeartbeat handler and the vaultkeeper.HeartbeatParent request method.
// Also tests that a cvk will remain registered with the autoheartbeater running and will be autopruned if the autoheartbeater is not running.
func Test_HeartBeatParent(t *testing.T) {
	const (
		parentCVKPruneTime = 100 * time.Millisecond
		childHeartbeatFreq = 20 * time.Millisecond
		badHBLimit         = 2
	)

	parent, err := New(rand.Uint64(), netip.MustParseAddrPort("[::0]:"+strconv.FormatUint(uint64(misc.RandomPort()), 10)),
		WithDragonsHoard(1), WithPruneTimes(PruneTimes{ChildVK: parentCVKPruneTime}))
	if err != nil {
		t.Fatal(err)
	} else if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(parent.Stop)
	child, err := New(rand.Uint64(), netip.MustParseAddrPort("[::0]:"+strconv.FormatUint(uint64(misc.RandomPort()), 10)),
		WithCustomHeartbeats(true, childHeartbeatFreq, badHBLimit))
	if err != nil {
		t.Fatal(err)
	}
	// note the lack of start on child at first

	// join child to parent
	if pVKID, ver, ack, err := client.Hello(t.Context(), child.ID(), parent.Address()); err != nil {
		t.Fatal(err)
	} else if pVKID != parent.ID() {
		t.Error("bad id", ExpectedActual(parent.ID(), pVKID))
	} else if ver != parent.versionSet.HighestSupported() {
		t.Error("bad version", ExpectedActual(parent.versionSet.HighestSupported(), ver))
	} else if ack.Height != uint32(parent.Height()) {
		t.Error("bad height", ExpectedActual(uint32(parent.Height()), ack.Height))
	}
	if err := child.Join(t.Context(), parent.Address()); err != nil {
		t.Fatal("failed to join child under parent:", err)
	}
	// ensure child is in the parent's table
	parent.children.mu.Lock()
	if v, found := parent.children.cvks.Load(child.ID()); !found {
		t.Error("failed to find child in parent's table")
	} else if len(v.services) != 0 {
		t.Error("child has services registered to it, but should have none")
	}
	parent.children.mu.Unlock()
	// ensure the child knows about its parent
	checkParent(t, child, struct {
		height     uint16
		parentID   slims.NodeID
		parentAddr netip.AddrPort
	}{0, parent.ID(), parent.Address()})
	// allow the child to get pruned due to the heartbeater not running (as it has not been started)
	time.Sleep(parentCVKPruneTime + 1*time.Millisecond)
	// confirm the child is not considered a child by the parent
	parent.children.mu.Lock()
	if _, found := parent.children.cvks.Load(child.ID()); found {
		t.Error("child should have been pruned")
	}
	parent.children.mu.Unlock()
	// ensure the child still believes it has a parent
	checkParent(t, child, struct {
		height     uint16
		parentID   slims.NodeID
		parentAddr netip.AddrPort
	}{0, parent.ID(), parent.Address()})
	// start the child
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.Stop)
	// allow plenty of time for the autoheartbeater to hit the parent, receive bad requests, and prune the parent from the child
	time.Sleep(childHeartbeatFreq * (badHBLimit * 2))
	// confirm that the child no longer considers itself to have a parent
	checkParent(t, child, struct {
		height     uint16
		parentID   slims.NodeID
		parentAddr netip.AddrPort
	}{0, 0, netip.AddrPort{}})

	// have the child rejoin, now that its heartbeater is active
	if pVKID, ver, ack, err := client.Hello(t.Context(), child.ID(), parent.Address()); err != nil {
		t.Fatal(err)
	} else if pVKID != parent.ID() {
		t.Error("bad id", ExpectedActual(parent.ID(), pVKID))
	} else if ver != parent.versionSet.HighestSupported() {
		t.Error("bad version", ExpectedActual(parent.versionSet.HighestSupported(), ver))
	} else if ack.Height != uint32(parent.Height()) {
		t.Error("bad height", ExpectedActual(uint32(parent.Height()), ack.Height))
	}
	if err := child.Join(t.Context(), parent.Address()); err != nil {
		t.Fatal("failed to join child under parent:", err)
	}
	// ensure child is in the parent's table
	parent.children.mu.Lock()
	if v, found := parent.children.cvks.Load(child.ID()); !found {
		t.Error("failed to find child in parent's table")
	} else if len(v.services) != 0 {
		t.Error("child has services registered to it, but should have none")
	}
	parent.children.mu.Unlock()
	// ensure the child knows about its parent
	checkParent(t, child, struct {
		height     uint16
		parentID   slims.NodeID
		parentAddr netip.AddrPort
	}{0, parent.ID(), parent.Address()})
	// wait for prune time to elapse to ensure child is still.... actually a child
	time.Sleep(parentCVKPruneTime * 2)
	// ensure child is in the parent's table
	parent.children.mu.Lock()
	if v, found := parent.children.cvks.Load(child.ID()); !found {
		t.Error("failed to find child in parent's table")
	} else if len(v.services) != 0 {
		t.Error("child has services registered to it, but should have none")
	}
	parent.children.mu.Unlock()
	// ensure the child knows about its parent
	checkParent(t, child, struct {
		height     uint16
		parentID   slims.NodeID
		parentAddr netip.AddrPort
	}{0, parent.ID(), parent.Address()})
}

// checkParent tests that that the values in vk match the values in expected.
//
// ! Acquires and releases vk's structure lock.
func checkParent(t *testing.T, vk *VaultKeeper, expected struct {
	height     uint16
	parentID   slims.NodeID
	parentAddr netip.AddrPort
}) {
	vk.structure.mu.RLock()
	if vk.structure.height != expected.height {
		t.Error("bad height", ExpectedActual(expected.height, vk.structure.height))
	} else if vk.structure.parentID != expected.parentID {
		t.Error("bad parent ID", ExpectedActual(expected.parentID, vk.structure.parentID))
	} else if vk.structure.parentAddr != expected.parentAddr {
		t.Error("bad parent addr", ExpectedActual(expected.parentAddr, vk.structure.parentAddr))
	}
	vk.structure.mu.RUnlock()
}

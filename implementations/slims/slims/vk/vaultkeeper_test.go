package vaultkeeper

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// Starts and stops a VK back to back, checking that we can successfully send/receive a STATUS message after each start and cannot do so after each stop
func TestVaultKeeper_StartStop(t *testing.T) {
	var (
		vkid slims.NodeID = 1
	)
	vk, err := New(vkid, RandomLocalhostAddrPort())
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
	vk.net.mu.RLock()
	defer vk.net.mu.RUnlock()
	alive = vk.net.accepting

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
	var rcvrAddr = RandomLocalhostAddrPort().String()
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
		originalMessageType = pb.MessageType_HELLO
		errno               = pb.Fault_BODY_REQUIRED
	)
	var (
		extraInfo      = []string{"1", "2", "3"}
		vkAddr         = RandomLocalhostAddrPort()
		expectedHeader = protocol.Header{
			Version: version.Version{Major: 2},
			Type:    pb.MessageType_FAULT,
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

	// send the fault packet to our dummy listener
	parsedRcvrAddr, err := net.ResolveUDPAddr("udp", rcvrAddr)
	if err != nil {
		t.Fatal(err)
	}
	vk.respondError(parsedRcvrAddr, originalMessageType, errno, extraInfo...)

	// receive and test
	res := <-ch
	if res.err != nil {
		t.Fatal(err)
	}
	if sa := netip.MustParseAddrPort(res.senderAddr.String()); sa.Compare(vkAddr) != 0 {
		t.Error(ExpectedActual(vkAddr, sa))
	}
	if res.header != expectedHeader {
		t.Error(ExpectedActual(expectedHeader, res.header))
	}
	bd := &pb.Fault{}
	if err := proto.Unmarshal(res.respBody, bd); err != nil {
		t.Fatal(err)
	}
	if bd.Original != originalMessageType {
		t.Error(ExpectedActual(originalMessageType, bd.Original))
	}
	if bd.Errno != errno {
		t.Error(ExpectedActual(errno, bd.Errno))
	}
	if bd.AdditionalInfo == nil || strings.TrimSpace(*bd.AdditionalInfo) == "" {
		t.Fatal("additional info is empty")
	} else {
		// check that each line contains its index+1
		var i int64 = 1
		for line := range strings.Lines(*bd.AdditionalInfo) {
			line = strings.TrimSpace(line)
			if parsed, err := strconv.ParseInt(line, 10, 8); err != nil {
				t.Fatal(err)
			} else if parsed != i {
				t.Errorf("bad value in line %v."+ExpectedActual(i, parsed), i)
			}
			i += 1
		}
	}

}

// Ensures that the data returned by respondSuccess looks as we expect it to.
func Test_respondSuccess(t *testing.T) {
	var rcvrAddr = RandomLocalhostAddrPort().String()
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
			return
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
		vkAddr  = RandomLocalhostAddrPort()
		sentHdr = protocol.Header{
			Version: version.Version{Major: 1, Minor: 12},
			Type:    pb.MessageType_HELLO_ACK,
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
		t.Fatal(res.err)
	}
	if sa := netip.MustParseAddrPort(res.senderAddr.String()); sa.Compare(vkAddr) != 0 {
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
	var vkid = UniqueID()

	// spin up a VK
	ddl, _ := t.Deadline()
	vk, err := New(vkid, RandomLocalhostAddrPort(),
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
			clientID := UniqueID()
			respVKID, respVKVersion, ack, err := client.Hello(t.Context(), clientID, vk.Address())
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
			vkid    = UniqueID()
			timeout = 30 * time.Millisecond
		)
		// spin up a VK
		vk, err := New(vkid, RandomLocalhostAddrPort(), WithPruneTimes(PruneTimes{Hello: timeout}))
		if err != nil {
			t.Fatal(err)
		} else if err := vk.Start(); err != nil {
			t.Fatal(err)
		}
		// send a hello to the vk
		clientID := UniqueID()
		respVKID, respVKVersion, ack, err := client.Hello(t.Context(), clientID, vk.Address())
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
		time.Sleep(timeout + PruneBuffer)
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
		UniqueID(),
		RandomLocalhostAddrPort(),
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
			UniqueID(),
			RandomLocalhostAddrPort(),
		)
		if err != nil {
			t.Fatal(err)
		} else if err := childVK.Start(); err != nil {
			t.Fatal(err)
		}
		defer childVK.Stop()

		// join the child under the parent
		t.Logf("child (%d) JOINing parent (%d)", childVK.ID(), parentVK.ID())
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
	{ // join a leaf under parent
		leafID := UniqueID()
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
		if vkID, accept, err := client.Join(t.Context(), leafID, parentVK.Address(), client.JoinInfo{IsVK: false, VKAddr: netip.AddrPort{}, Height: 0}); err != nil {
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
	// test to ensure that services already registered on a child are then registered to the parent (and not pruned) after joining
	t.Run("services propagates to parent", func(t *testing.T) {
		const (
			serviceStaleTime = 1 * time.Second
			bufferTime       = 50 * time.Millisecond // extra time to give after stale to give the pruner time to run
		)
		nop := zerolog.Nop()
		LeafA, LeafB := Leaf{ID: UniqueID(), Services: map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}{
			randomdata.City(): {serviceStaleTime, RandomLocalhostAddrPort()},
		}}, Leaf{ID: UniqueID(), Services: map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}{
			randomdata.Day(): {serviceStaleTime, RandomLocalhostAddrPort()},
		}}
		cVK, err := New(UniqueID(), RandomLocalhostAddrPort(), WithLogger(&nop))
		if err != nil {
			t.Fatal(err)
		} else if err := cVK.Start(); err != nil {
			t.Fatal(err)
		}
		// register services to the cVK and set up automated heartbeating
		if _, err := client.RegisterNewLeaf(t.Context(), LeafA.ID, cVK.Address(), LeafA.Services); err != nil {
			t.Fatal(err)
		} else if _, err := client.RegisterNewLeaf(t.Context(), LeafB.ID, cVK.Address(), LeafB.Services); err != nil {
			t.Fatal(err)
		}
		hbErr := make(chan error, 10)
		hbACancel, err := client.AutoServiceHeartbeat(
			300*time.Millisecond,
			serviceStaleTime/2,
			LeafA.ID,
			client.ParentInfo{ID: cVK.ID(), Addr: cVK.Address()}, slices.Collect(maps.Keys(LeafA.Services)), hbErr)
		if err != nil {
			t.Fatal(err)
		}
		hbBCancel, err := client.AutoServiceHeartbeat(
			300*time.Millisecond,
			serviceStaleTime/2,
			LeafB.ID,
			client.ParentInfo{ID: cVK.ID(), Addr: cVK.Address()}, slices.Collect(maps.Keys(LeafB.Services)), hbErr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(hbACancel)
		t.Cleanup(hbBCancel)
		// await at least one heartbeat to ensure no errors occur
		select {
		case <-time.After(serviceStaleTime + bufferTime):
		case err := <-hbErr:
			t.Error(err)
			// drain the rest of the error
			for err := range hbErr {
				t.Error(err)
			}
			t.FailNow()
		}
		// join the cVK to the parent vk and check that services propagated upward
		if err := cVK.Join(t.Context(), parentVK.Address()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(bufferTime) // give a moment for the services to propagate
		for svc, inf := range LeafA.Services {
			if providers, found := parentVK.children.allServices[svc]; !found {
				t.Errorf("did not find LeafA service %v on parent", svc)
			} else if addr, found := providers[cVK.ID()]; !found {
				t.Errorf("cVK %v is not a provider of LeafA service %v on parent", cVK.ID(), svc)
			} else if addr != inf.Addr {
				t.Errorf("bad address on LeafA service %v: %s", svc, ExpectedActual(inf.Addr, addr))
			}
		}

		// allow one of the leaves to age out from cVK and ensure the parent learns this too
		{
			hbBCancel()
			time.Sleep(serviceStaleTime + bufferTime)
			parentSnap := parentVK.Snapshot()
			cvkInf, found := parentSnap.Children.CVKs[cVK.ID()]
			if !found {
				t.Fatal("cVK is considered not a provider of any services, but should still be offering the services of leafA", parentSnap.Children)
			}
			// all leafA services should be found
			for svc := range LeafA.Services {
				if _, found := cvkInf.Services[svc]; !found {
					t.Errorf("failed to find LeafA service '%s' at parent", svc)
				}
			}
			// no leafB services should be found
			for svc := range LeafB.Services {
				if _, found := cvkInf.Services[svc]; found {
					t.Errorf("found LeafB service '%s' at parent (should have been deregistered)", svc)
				}
			}
		}
	})
}

// Tests both the serveVKHeartbeat handler and the vaultkeeper.HeartbeatParent request method.
// Also tests that a cvk will remain registered with the autoheartbeater running and will be autopruned if the autoheartbeater is not running.
func Test_HeartBeatParent(t *testing.T) {
	const (
		parentCVKPruneTime = 100 * time.Millisecond
		childHeartbeatFreq = 20 * time.Millisecond
		badHBLimit         = 2
	)

	parent, err := New(UniqueID(), RandomLocalhostAddrPort(),
		WithDragonsHoard(1), WithPruneTimes(PruneTimes{ChildVK: parentCVKPruneTime}))
	if err != nil {
		t.Fatal(err)
	} else if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(parent.Stop)
	child, err := New(UniqueID(), RandomLocalhostAddrPort(),
		WithCustomHeartbeats(true, childHeartbeatFreq, badHBLimit))
	if err != nil {
		t.Fatal(err)
	}
	// note the lack of start on child at first

	// join child to parent
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
	time.Sleep(parentCVKPruneTime + PruneBuffer)
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
		t.Errorf("failed to find child %v in parent's table", child.ID())
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

// As it says on the tin, Test_MergeIncrement checks thats vks can merge and increment their children.
func Test_MergeIncrement(t *testing.T) {
	t.Run("merge: 0h-0h", func(t *testing.T) {
		t.Parallel()

		const cVKPrune = 500 * time.Millisecond
		// spawn two vks
		vkNewRoot, err := New(UniqueID(), RandomLocalhostAddrPort(),
			WithPruneTimes(PruneTimes{
				Hello:           300 * time.Millisecond,
				ServicelessLeaf: 10 * time.Minute,
				ChildVK:         cVKPrune,
			}))
		if err != nil {
			t.Fatal(err)
		} else if err := vkNewRoot.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(vkNewRoot.Stop)
		vkOldRoot, err := New(UniqueID(), RandomLocalhostAddrPort(), WithCustomHeartbeats(true, 300*time.Millisecond, 3))
		if err != nil {
			t.Fatal(err)
		} else if err := vkOldRoot.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(vkOldRoot.Stop)
		// merge them
		if err := vkNewRoot.Merge(vkOldRoot.Address()); err != nil {
			t.Fatal(err)
		}
		{
			// check that the new child is recognized by the new root
			vkNewRoot.children.mu.Lock()
			if _, found := vkNewRoot.children.cvks.Load(vkOldRoot.ID()); !found {
				vkNewRoot.children.mu.Unlock()
				t.Fatal("new root does not recognize old root as a child")
			}
			vkNewRoot.children.mu.Unlock()
			// check that the new child recognizes its parent
			vkOldRoot.structure.mu.RLock()
			if vkOldRoot.structure.parentAddr.String() != vkNewRoot.addr.String() || vkOldRoot.structure.parentID != vkNewRoot.id {
				vkOldRoot.structure.mu.RUnlock()
				t.Fatalf("old root does not recognize new root as its parent.\n New root info: %d@%v | Old root's parent info: %d@%v",
					vkNewRoot.id, vkNewRoot.addr,
					vkOldRoot.structure.parentID,
					vkOldRoot.structure.parentAddr,
				)
			}
			vkOldRoot.structure.mu.RUnlock()
		}
		// wait to ensure heartbeating works
		time.Sleep(cVKPrune + PruneBuffer)
		{ // check that the new child is still recognized
			vkNewRoot.children.mu.Lock()
			if _, found := vkNewRoot.children.cvks.Load(vkOldRoot.ID()); !found {
				vkNewRoot.children.mu.Unlock()
				t.Fatal("new root does not recognize old root as a child")
			}
			vkNewRoot.children.mu.Unlock()

			vkOldRoot.structure.mu.RLock()
			if vkOldRoot.structure.parentAddr.String() != vkNewRoot.addr.String() || vkOldRoot.structure.parentID != vkNewRoot.id {
				vkOldRoot.structure.mu.RUnlock()
				t.Fatalf("old root does not recognize new root as its parent.\n New root info: %d@%v | Old root's parent info: %d@%v",
					vkNewRoot.id, vkNewRoot.addr,
					vkOldRoot.structure.parentID,
					vkOldRoot.structure.parentAddr,
				)
			}
			vkOldRoot.structure.mu.RUnlock()
		}
	})
	t.Run("merge: 0h-1h incorrect heights", func(t *testing.T) {
		vkH1 := spawnVK(t, Leaf{}, WithDragonsHoard(1))
		t.Cleanup(vkH1.Stop)
		vkH0 := spawnVK(t, Leaf{})
		if err := vkH0.Merge(vkH1.Address()); err == nil {
			t.Fatalf("expected error %s, got nil", pb.Fault_BAD_HEIGHT.String())
		}
		if err := vkH1.Merge(vkH0.Address()); !errors.Is(err, slims.Errno{Num: pb.Fault_BAD_HEIGHT}) {
			t.Fatalf("expected error %s(%d), got %v", pb.Fault_BAD_HEIGHT.String(), pb.Fault_BAD_HEIGHT.Number(), err)
		}
	})
	t.Run("manual increment", func(t *testing.T) {
		// merge two h0s to build the tree
		parentVK := spawnVK(t, Leaf{})
		t.Cleanup(parentVK.Stop)
		childVK := spawnVK(t, Leaf{})
		t.Cleanup(childVK.Stop)
		if err := parentVK.Merge(childVK.Address()); err != nil {
			t.Fatal(err)
		}

		thirdVK := spawnVK(t, Leaf{}, WithDragonsHoard(1))
		t.Cleanup(thirdVK.Stop)

		t.Run("reject increment from non-parent", func(t *testing.T) {
			var cAddr *net.UDPAddr
			if cAddr = net.UDPAddrFromAddrPort(childVK.Address()); cAddr == nil {
				t.Fatal("failed to parse childVK address into UDPAddr")
			}

			if err := thirdVK.increment(cAddr); !errors.Is(err, slims.Errno{Num: pb.Fault_NOT_PARENT}) {
				t.Fatal(ExpectedActual(error(slims.Errno{Num: pb.Fault_NOT_PARENT}), err))
			}
		})
		t.Run("successful increment after new merge", func(t *testing.T) {
			// childVK(0) -> parentVK(1)	thirdVK(1)
			// should become
			// childVK(1) -> parentVK(2) <- thirdVK(1)

			// sanity check child's precursor height
			if childVK.Height() != 0 {
				t.Fatal("failed sanity check", ExpectedActual(0, childVK.Height()))
			}

			if err := parentVK.Merge(thirdVK.Address()); err != nil {
				t.Fatal(err)
			}
			// check that the original child knows that the vault has grown
			if childVK.Height() != 1 {
				t.Fatal("bad child height post-Merge", ExpectedActual(1, childVK.Height()))
			}
		})
	})
}

// checkParent tests that that the values in vk match the values in expected.
//
// ! Acquires and releases vk's structure lock.
func checkParent(t *testing.T, vk *VaultKeeper, expected struct {
	height     uint16
	parentID   slims.NodeID
	parentAddr netip.AddrPort
}) {
	t.Helper()
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

// spawnResponseListener spools up a *unbuffered* UDP packet listener that echoes each packet it receives over the returned channel.
// Because the listener is unbuffered, it will block the listener until the channel is consumed.
//
// Can be cancelled with the returned function.
// The listener only stops (and then closes the channel) after cancel() is invoked.
// Calls Fatal if it fails to create the listener.
func spawnResponseListener(t *testing.T) (requestorAddr *net.UDPAddr, resp chan struct {
	n        int             // number of bytes in the packet
	origAddr net.Addr        // originator address
	hdr      protocol.Header // packet header
	body     []byte          // packet body
	err      error           // error listening
}, cancel func()) {
	t.Helper()
	// spin up a listener to receive responses and a channel to forward them on
	earAP := RandomLocalhostAddrPort()
	requestorAddr = net.UDPAddrFromAddrPort(earAP)
	ctx, cancel := context.WithCancel(t.Context())
	pconn, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp", earAP.String())
	if err != nil {
		t.Fatal(err)
	}
	resp = make(chan struct {
		n        int
		origAddr net.Addr
		hdr      protocol.Header
		body     []byte
		err      error
	})
	// send all packets received across the channel
	go func() {
		for ctx.Err() == nil {
			n, origAddr, hdr, body, err := protocol.ReceivePacket(pconn, t.Context())
			resp <- struct {
				n        int
				origAddr net.Addr
				hdr      protocol.Header
				body     []byte
				err      error
			}{n, origAddr, hdr, body, err}
		}
		close(resp)
	}()

	return requestorAddr, resp, cancel
}

func Test_serveRegister(t *testing.T) {
	const servicelessLeafPT time.Duration = 400 * time.Millisecond

	requestorAddr, respCh, cancel := spawnResponseListener(t)
	defer cancel()
	t.Logf("requestor address: %v", requestorAddr)

	tests := []struct {
		name      string
		sendHELLO bool
		join      struct {
			send bool            // send one at all?
			req  client.JoinInfo // info to compose the join
		}
		registerDelay time.Duration   // how long to wait after sending a JOIN (or when a JOIN would have been sent) before sending the register
		header        protocol.Header // header used for register
		body          proto.Message   // body to send with register
		wantError     struct {
			erred bool
			errno pb.Fault_Errnos
			// don't bother checking additional info
		}
	}{
		{"no stale time",
			true,
			struct {
				send bool
				req  client.JoinInfo
			}{
				false,
				client.JoinInfo{IsVK: false, VKAddr: netip.AddrPort{}, Height: 0}},
			0, protocol.Header{
				Version: defaultVersionsSupported.HighestSupported(),
				Type:    pb.MessageType_REGISTER,
				ID:      UniqueID(),
			}, &pb.Register{
				Service: randomdata.City(),
				Address: "1.1.1.1:1",
			}, struct {
				erred bool
				errno pb.Fault_Errnos
			}{true, pb.Fault_BAD_STALE_TIME},
		},
		{"only HELLO",
			true,
			struct {
				send bool
				req  client.JoinInfo
			}{
				false,
				client.JoinInfo{IsVK: false, VKAddr: netip.AddrPort{}, Height: 0}},
			0, protocol.Header{
				Version: defaultVersionsSupported.HighestSupported(),
				Type:    pb.MessageType_REGISTER,
				ID:      UniqueID(),
			}, &pb.Register{
				Service: randomdata.City(),
				Address: "1.1.1.1:1",
				Stale:   "3m",
			}, struct {
				erred bool
				errno pb.Fault_Errnos
			}{true, pb.Fault_UNKNOWN_CHILD_ID},
		},
		{"simple",
			true,
			struct {
				send bool
				req  client.JoinInfo
			}{
				true,
				client.JoinInfo{IsVK: false, VKAddr: netip.AddrPort{}, Height: 0}},
			0, protocol.Header{
				Version: defaultVersionsSupported.HighestSupported(),
				Type:    pb.MessageType_REGISTER,
				ID:      UniqueID(),
			}, &pb.Register{
				Service: randomdata.City(),
				Address: "1.1.1.1:1",
				Stale:   "3m",
			}, struct {
				erred bool
				errno pb.Fault_Errnos
			}{false, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// spin up the vk
			vk, err := New(UniqueID(), RandomLocalhostAddrPort(),
				WithPruneTimes(PruneTimes{10 * time.Second, servicelessLeafPT, 10 * time.Second}),
				WithLogger(&zerolog.Logger{}), // suppress logging
			)
			if err != nil {
				t.Fatal(err)
			} else if err := vk.Start(); err != nil {
				t.Fatal(err)
			}
			defer vk.Stop()
			t.Logf("vk address: %v", vk.Address())

			// send a HELLO (if applicable)
			if tt.sendHELLO {
				// only bother to check err; the other tests check hello for us quite a bit
				if _, _, _, err := client.Hello(t.Context(), tt.header.ID, vk.Address()); err != nil {
					t.Fatal("failed to HELLO:", err)
				}
			}
			if tt.join.send {
				// only bother to check err; the other tests check join for us quite a bit
				if _, _, err := client.Join(t.Context(), tt.header.ID, vk.Address(), tt.join.req); err != nil {
					t.Fatal("failed to JOIN:", err)
				}
			}
			if tt.registerDelay > 0 {
				time.Sleep(tt.registerDelay)
			}

			// craft a packet
			bd, err := proto.Marshal(tt.body)
			if err != nil {
				t.Fatal(err)
			}
			t.Log("sending packet to serveRegister")
			// use the requestor addr to receive the packet echo
			erred, errno, _ := vk.serveRegister(tt.header, bd, requestorAddr)
			if erred && !tt.wantError.erred {
				t.Fatalf("serveRegister failed, but was not expected to. errno: %v", errno)
			} else if erred && tt.wantError.erred {
				// expected error, check that it is the right kind
				if errno != tt.wantError.errno {
					t.Fatal("bad errno", ExpectedActual(tt.wantError.errno, errno))
				}
			} else { // did not error, did not expect error
				t.Log("awaiting response...")
				// we only need to capture successes
				registerResp := <-respCh
				t.Log("received response.")
				if registerResp.err != nil {
					t.Fatal(err)
				}
				if registerResp.origAddr.String() != vk.addr.String() {
					t.Error("bad address", ExpectedActual(vk.addr.String(), registerResp.origAddr.String()))
				}
				if registerResp.hdr.ID != vk.ID() {
					t.Error("bad vk ID", ExpectedActual(vk.ID(), registerResp.hdr.ID))
				}
				if registerResp.hdr.Type != pb.MessageType_REGISTER_ACCEPT {
					t.Error("bad response message type", ExpectedActual(pb.MessageType_REGISTER_ACCEPT, registerResp.hdr.Type))
				}
				// unpack the body
				var accept pb.RegisterAccept
				if err := pbun.Unmarshal(registerResp.body, &accept); err != nil {
					t.Fatal("failed to unmarshal: ", err)
				}

				coerced, ok := tt.body.(*pb.Register)
				if !ok {
					t.Fatal("failed to coerce body sent with test as a Register pointer")
				}
				if accept.Service != coerced.Service {
					t.Error("bad service echo", ExpectedActual(coerced.Service, accept.Service))
				}
			}
		})
	}

	t.Run("propagate to parent", func(t *testing.T) {
		cLeaf := Leaf{ID: 100,
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				"cs1": {
					Stale: 3 * time.Second,
					Addr:  RandomLocalhostAddrPort(),
				},
				"cs2": {
					Stale: 3 * time.Second,
					Addr:  RandomLocalhostAddrPort(),
				},
				"cs3": {
					Stale: 3 * time.Second,
					Addr:  RandomLocalhostAddrPort(),
				},
			},
		}
		pLeaf := Leaf{ID: 200,
			Services: map[string]struct {
				Stale time.Duration
				Addr  netip.AddrPort
			}{
				"ps1": {
					Stale: 3 * time.Second,
					Addr:  RandomLocalhostAddrPort(),
				},
				"ps2": {
					Stale: 3 * time.Second,
					Addr:  RandomLocalhostAddrPort(),
				},
			},
		}

		// spawn a vk and assign it a parent
		cVK, err := New(1, RandomLocalhostAddrPort(), WithLogger(nil))
		if err != nil {
			t.Fatal(err)
		} else if err := cVK.Start(); err != nil {
			t.Fatal(err)
		}
		pVK, err := New(2, RandomLocalhostAddrPort(), WithLogger(nil), WithDragonsHoard(1))
		if err != nil {
			t.Fatal(err)
		} else if err := pVK.Start(); err != nil {
			t.Fatal(err)
		}
		if err := cVK.Join(t.Context(), pVK.Address()); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := client.Hello(t.Context(), cVK.ID(), pVK.Address()); err != nil {
			t.Fatal(err)
		}
		// register leaf and its services to cVK
		if _, _, _, err := client.Hello(t.Context(), cLeaf.ID, cVK.Address()); err != nil {
			t.Fatal(err)
		}
		if _, _, err := client.Join(t.Context(), cLeaf.ID, cVK.Address(), client.JoinInfo{}); err != nil {
			t.Fatal(err)
		}
		for svc, inf := range cLeaf.Services {
			if _, _, err := client.Register(t.Context(), cLeaf.ID, cVK.Address(), svc, inf.Addr, inf.Stale); err != nil {
				t.Fatal(err)
			}
		}
		// register leaf and its services to pVK
		if registered, err := client.RegisterNewLeaf(t.Context(), pLeaf.ID, pVK.Address(), pLeaf.Services); err != nil {
			t.Fatal(err)
		} else if svcs := slices.Collect(maps.Keys(pLeaf.Services)); !SlicesUnorderedEqual(svcs, registered) {
			t.Fatal("services registered mismatch", ExpectedActual(svcs, registered))
		}
		if _, _, _, err := client.Hello(t.Context(), pLeaf.ID, pVK.Address()); err != nil {
			t.Fatal(err)
		}
		if _, _, err := client.Join(t.Context(), pLeaf.ID, pVK.Address(), client.JoinInfo{}); err != nil {
			t.Fatal(err)
		}
		for svc, inf := range pLeaf.Services {
			if _, _, err := client.Register(t.Context(), pLeaf.ID, pVK.Address(), svc, inf.Addr, inf.Stale); err != nil {
				t.Fatal(err)
			}
		}
		// check that the child has only the child leaf service associated
		{
			snap := cVK.Snapshot()
			if lservices, found := snap.Children.Leaves[cLeaf.ID]; !found {
				t.Fatalf("failed to find cLeaf %v on cVK (children: %v)", cLeaf.ID, snap.Children)
			} else if !maps.Equal(lservices, cLeaf.Services) {
				t.Fatalf("services mismatch.\ncLeaf services: %v\n associated services found in snapshot: %v\n", cLeaf.Services, lservices)
			}
		}
		// check that the parent has both child and parent leaf services associated
		{
			snap := pVK.Snapshot()
			if lservices, found := snap.Children.Leaves[pLeaf.ID]; !found {
				t.Fatalf("failed to find pLeaf %v on pVK (children: %v)", pLeaf.ID, snap.Children)
			} else if !maps.Equal(lservices, pLeaf.Services) { // check leaf services
				t.Fatalf("services mismatch.\npLeaf services: %v\n associated services found in snapshot: %v\n", pLeaf.Services, lservices)
			}
			if vkServices, found := snap.Children.CVKs[cVK.ID()]; !found {
				t.Fatalf("failed to find cvk %v in list of children (%v)", cVK.ID(), snap.Children)
			} else {
				for svc := range cLeaf.Services {
					if _, found := vkServices.Services[svc]; !found {
						t.Errorf("child leaf service %v not found via child VK", svc)
					}
				}
				if t.Failed() {
					return
				}
			}
		}

	})

}

// Each test spins up a VK and crafts serviceHeartbeat packets to send to it, directing the response over a dummy response listener.
// Validates the response packet matches the information contained in the VK.
// NOTE(rlandau): the dummy response listener is shared across all tests.
func Test_serveServiceHeartbeat(t *testing.T) {
	requestorAddr, respCh, cancel := spawnResponseListener(t)
	defer cancel()
	t.Logf("requestor address: %v", requestorAddr)

	// can be used in test creation to ensure that the ID in the header matches the ID of one or more registered services
	staticID := UniqueID()

	tests := []struct {
		name               string
		registeredServices map[uint64]string // leaf ID the service is registered to -> the name of the service
		reqHdr             protocol.Header
		reqBody            *pb.ServiceHeartbeat
		expectedError      bool
		expectedErrno      pb.Fault_Errnos
		expectedBody       *pb.ServiceHeartbeatAck // only checked if expectedType is mt.ServiceHeartbeatAck
	}{
		{"shorthand",
			nil,
			protocol.Header{
				Version:   protocol.SupportedVersions().HighestSupported(),
				Shorthand: true,
				Type:      pb.MessageType_SERVICE_HEARTBEAT,
				ID:        UniqueID(),
			}, &pb.ServiceHeartbeat{},
			true,
			pb.Fault_SHORTHAND_NOT_ACCEPTED,
			nil,
		},
		{"no body",
			nil,
			protocol.Header{
				Version: version.Version{Major: 0, Minor: 0},
				Type:    pb.MessageType_SERVICE_HEARTBEAT,
				ID:      UniqueID(),
			}, &pb.ServiceHeartbeat{},
			true,
			pb.Fault_BODY_REQUIRED,
			nil,
		},
		{"bad version",
			nil,
			protocol.Header{
				Version: version.Version{Major: 0, Minor: 0},
				Type:    pb.MessageType_SERVICE_HEARTBEAT,
				ID:      UniqueID(),
			}, &pb.ServiceHeartbeat{Services: []string{"dummy value"}},
			true,
			pb.Fault_VERSION_NOT_SUPPORTED,
			nil,
		},
		{"no services registered (equivalent to no services registered to requesting parent)",
			nil,
			protocol.Header{
				Version: protocol.SupportedVersions().HighestSupported(),
				Type:    pb.MessageType_SERVICE_HEARTBEAT,
				ID:      UniqueID(),
			}, &pb.ServiceHeartbeat{Services: []string{randomdata.Currency(), randomdata.Currency()}},
			true,
			pb.Fault_UNKNOWN_CHILD_ID,
			nil,
		},
		{"simple success",
			map[uint64]string{staticID: "some service"},
			protocol.Header{
				Version: protocol.SupportedVersions().HighestSupported(),
				Type:    pb.MessageType_SERVICE_HEARTBEAT,
				ID:      staticID,
			},
			&pb.ServiceHeartbeat{Services: []string{"some service"}},
			false,
			0,
			&pb.ServiceHeartbeatAck{Refresheds: []string{"some service"}},
		},
		{"all unknown",
			map[uint64]string{staticID: "some service"},
			protocol.Header{
				Version: protocol.SupportedVersions().HighestSupported(),
				Type:    pb.MessageType_SERVICE_HEARTBEAT,
				ID:      staticID,
			},
			&pb.ServiceHeartbeat{Services: []string{"misspelled service"}},
			true,
			pb.Fault_ALL_UNKNOWN,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// spin up the vk
			vk, err := New(UniqueID(), RandomLocalhostAddrPort(),
				WithPruneTimes(PruneTimes{10 * time.Second, 10 * time.Second, 10 * time.Second}),
				WithLogger(&zerolog.Logger{}), // suppress logging
			)
			if err != nil {
				t.Fatal(err)
			} else if err := vk.Start(); err != nil {
				t.Fatal(err)
			}
			defer vk.Stop()
			t.Logf("vk address: %v", vk.Address())
			// create required services
			for leafID, service := range tt.registeredServices {
				// register leaf
				if vk.addLeaf(leafID) {
					t.Fatal("bad result from addLeaf: cvk overlap but no cvks are registered")
				}
				// register service
				if erred, errno, ei := vk.addService(leafID, service, netip.MustParseAddrPort("127.0.0.1:0"), 10*time.Second); erred {
					t.Fatalf("failed to add service: #%v: (%v)", errno, ei)
				}
			}

			// marshal the body
			bd, err := proto.Marshal(tt.reqBody)
			if err != nil {
				t.Fatal(err)
			}
			erred, errno, _ := vk.serveServiceHeartbeat(tt.reqHdr, bd, requestorAddr)
			if erred {
				if !tt.expectedError {
					t.Fatalf("serveServiceHeartbeat failed, but was not expected to. errno: %v", errno)
				}
				if tt.expectedErrno != errno {
					t.Error("bad errno", ExpectedActual(tt.expectedErrno, errno))
				}

				return
			}
			vkResp := <-respCh
			// check for values set across all tests
			if vkResp.err != nil {
				t.Fatal(err)
			} else if vkResp.n == 0 {
				t.Fatal("a 0 byte response was found", vkResp)
			} else if vkResp.origAddr.String() != vk.addr.String() {
				t.Fatal(ExpectedActual(requestorAddr.String(), vkResp.origAddr.String()))
			} else if vkResp.hdr.ID != vk.ID() {
				t.Fatal(ExpectedActual(vk.ID(), vkResp.hdr.ID))
			}
			if vkResp.hdr.Type != pb.MessageType_SERVICE_HEARTBEAT_ACK {
				t.Fatal("unhandled message type")
			}
			var ack pb.ServiceHeartbeatAck
			if err := proto.Unmarshal(vkResp.body, &ack); err != nil {
				t.Fatal("failed to unmarshal body as service heartbeat")
			}
			if slices.Compare(ack.Refresheds, tt.expectedBody.Refresheds) != 0 {
				t.Error(ExpectedActual(tt.expectedBody.Refresheds, ack.Refresheds))
			}
		})
	}
}

// Tests both vk.Leave() and vk.serveLeave() handling.
func Test_Leave(t *testing.T) {
	const (
		serviceStale time.Duration = 30 * time.Second // disable service stale times
		leafPrune    time.Duration = 30 * time.Second // disable leaves being pruned
	)
	ctx, cnl := context.WithTimeout(t.Context(), 5*time.Second) // ensure most single operations occur within a reasonable timeframe
	t.Cleanup(cnl)
	// Create a linear, 3-node topology (vk0 -> vk1 -> vk2)
	l0 := Leaf{
		ID: UniqueID(),
		Services: map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}{
			"l0s1": {
				Stale: serviceStale,
				Addr:  RandomLocalhostAddrPort(),
			},
			"ssh": {
				Stale: serviceStale,
				Addr:  RandomLocalhostAddrPort(),
			},
		},
	}
	vk0 := spawnVK(t, l0, WithDragonsHoard(0), WithPruneTimes(PruneTimes{ServicelessLeaf: leafPrune}))
	t.Cleanup(vk0.Stop)
	l1 := Leaf{
		ID: UniqueID(),
		Services: map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}{
			"ssh": {
				Stale: serviceStale,
				Addr:  RandomLocalhostAddrPort(),
			},
		},
	}
	vk1 := spawnVK(t, l1, WithDragonsHoard(1), WithPruneTimes(PruneTimes{ServicelessLeaf: leafPrune}))
	t.Cleanup(vk1.Stop)
	l2 := Leaf{
		ID: UniqueID(),
		Services: map[string]struct {
			Stale time.Duration
			Addr  netip.AddrPort
		}{
			"l2s1": {
				Stale: serviceStale,
				Addr:  RandomLocalhostAddrPort(),
			},
		},
	}
	vk2 := spawnVK(t, l2, WithDragonsHoard(2), WithPruneTimes(PruneTimes{ServicelessLeaf: leafPrune}))
	t.Cleanup(vk2.Stop)
	{
		if err := vk0.Join(ctx, vk1.Address()); err != nil {
			t.Fatal(err)
		}
		if err := vk1.Join(ctx, vk2.Address()); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("topology generated")

	{ // check that leaf0's service exist on each vk
		snap := vk0.Snapshot()
		if services, found := snap.Children.Leaves[l0.ID]; !found {
			t.Fatal("failed to find child 0")
		} else if !maps.Equal(services, l0.Services) {
			t.Fatal("service mismatch", ExpectedActual(l0.Services, services))
		}
		snap = vk1.Snapshot()
		if inf, found := snap.Children.CVKs[vk0.ID()]; !found {
			t.Fatal("failed to find vk0 as a child of vk1")
		} else {
			for svc, f := range l0.Services {
				addr, found := inf.Services[svc]
				if !found {
					t.Fatalf("failed to find service %v (provided by vk0) on vk1", svc)
				} else if f.Addr != addr {
					t.Fatalf("bad address of service %v"+ExpectedActual(f.Addr, addr), svc)
				}
			}
		}
	}
	// have vk0 leave and confirm that its services are no longer available on vk1 or vk2
	vk0.Leave(300 * time.Millisecond)
	// check that vk0 has updated its parent correctly
	snap := vk0.Snapshot()
	if snap.ParentAddr.IsValid() || snap.ParentID != 0 {
		t.Error("vk0 did not update its parent information after leaving!")
	}
	time.Sleep(PruneBuffer)
	// ensure other services have not been affected
	if _, serviceAddr, err := client.Get(t.Context(), "ssh", vk2.Address(), "WhaleWhaleWhale", 1, nil); err != nil {
		t.Fatal(err)
	} else if serviceAddr == "" {
		t.Fatal("failed to find the 'ssh' service at root (vk2)")
	}
	if _, serviceAddr, err := client.Get(t.Context(), "l2s1", vk2.Address(), "WaterWeHaveHere", 1, nil); err != nil {
		t.Fatal(err)
	} else if serviceAddr == "" {
		t.Fatal("failed to find the 'l2s1' service at root (vk2)")
	}
	// ensure vk0's unique service is gone on both nodes
	if _, serviceAddr, err := client.Get(t.Context(), "l0s1", vk1.Address(), "KnockKnock", 1, nil); err != nil {
		t.Fatal(err)
	} else if serviceAddr != "" {
		t.Fatal("found the no-longer-active 'l0s1' service at mid (vk1)")
	}
	// ensure it was propagated upward
	if _, serviceAddr, err := client.Get(t.Context(), "l0s1", vk2.Address(), "WhosThere", 1, nil); err != nil {
		t.Fatal(err)
	} else if serviceAddr != "" {
		t.Fatalf("found the no-longer-active 'l0s1' service at root (vk2) (service address: %v)", serviceAddr)
	}
}

// helper function to spin up a vk, start it, and register the given leaf (and its services) under it.
// Fatal on error.
// Remember to defer vk.Stop().
// Attaches a 500ms timeout to each operation.
func spawnVK(t *testing.T, childLeaf Leaf, vkOpts ...VKOption) *VaultKeeper {
	t.Helper()
	ctx, cnl := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cnl()

	vk, err := New(UniqueID(), RandomLocalhostAddrPort(), vkOpts...)
	if err != nil {
		t.Fatal(err)
	} else if err = vk.Start(); err != nil {
		t.Fatal(err)
	}
	// TODO register t.Cleanup(vk.Stop)

	// only bother to spawn a leaf if an ID was given
	if childLeaf.ID == 0 {
		return vk
	}

	if _, _, _, err := client.Hello(ctx, childLeaf.ID, vk.Address()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Join(ctx, childLeaf.ID, vk.Address(), client.JoinInfo{IsVK: false}); err != nil {
		t.Fatal(err)
	}
	for service, info := range childLeaf.Services {
		if _, _, err := client.Register(ctx, childLeaf.ID, vk.Address(), service, info.Addr, info.Stale); err != nil {
			t.Fatal(err)
		}
		t.Logf("registered service %s from leaf %d on vk %d", service, childLeaf.ID, vk.ID())
	}

	return vk
}

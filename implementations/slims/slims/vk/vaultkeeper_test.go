package vaultkeeper

import (
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	. "github.com/rflandau/Orv/internal/testsupport"
	"google.golang.org/protobuf/proto"
)

func TestVaultKeeper_StartStop(t *testing.T) {
	var vkid slims.NodeID = 1
	vk, err := New(vkid, netip.MustParseAddrPort("127.0.0.1:8081"))
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

	const (
		reason string = "test"
	)
	var (
		vkAddr         = netip.MustParseAddrPort("127.0.0.1:8080")
		expectedHeader = protocol.Header{
			Version: protocol.HighestSupported,
			Type:    mt.Fault,
			ID:      1,
		}
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

func Test_respondSuccess(t *testing.T) {
	const rcvrAddr string = "127.0.0.1:8081"
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

	const (
		reason string = "test"
	)
	var (
		vkAddr  = netip.MustParseAddrPort("127.0.0.1:8080")
		sentHdr = protocol.Header{
			Version: protocol.HighestSupported,
			Type:    mt.HelloAck,
			ID:      1,
		}
		sentPayload = &pb.HelloAck{Height: 10, Version: uint32(protocol.VersionFromByte(0b01010001).Byte())}
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
	if rcvdPayload.Version != sentPayload.Version {
		t.Error(ExpectedActual(sentPayload.Version, rcvdPayload.Version))

	}
}

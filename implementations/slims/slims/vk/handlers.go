package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"net"
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"google.golang.org/protobuf/proto"
)

// handler is the core processing called for each request.
// When a request arrives, it is logged and the Orv header is deserialized from it.
// Version is validated, then the request is passed to the appropriate subhandler.

// serveStatus answers STATUS packets, returning a variety of information about the vk.
// Briefly holds a read lock on structure.
func (vk *VaultKeeper) serveStatus(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) {
	// no header validation is required

	// check that we were not given a body
	if len(reqBody) != 0 {
		vk.log.Warn().Int("body length", len(reqBody)).Str("body", string(bytes.TrimSpace(reqBody))).Msg("STATUS message has body")
		vk.respondError(senderAddr, ErrBodyNotAccepted(mt.Status).Error())
		return
	}

	vk.structure.mu.RLock()
	// gather data
	st := &pb.StatusResp{
		Height:            uint32(vk.structure.height),
		VersionsSupported: vk.versionSet.AsBytes(),
	}
	vk.structure.mu.RUnlock()

	// send data to client
	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(), Type: mt.StatusResp, ID: vk.id},
		st)
}

// serveHello answers HELLO packets with HELLO_ACK or FAULT.
// Selects a version based on the request version (the version in the header) and what versions we support.
func (vk *VaultKeeper) serveHello(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) {
	// validate parameters
	if reqHdr.Shorthand {
		vk.respondError(senderAddr, ErrShorthandNotAccepted(mt.Hello).Error())
		return
	} else if len(reqBody) != 0 {
		vk.log.Warn().Int("body length", len(reqBody)).Str("body", string(bytes.TrimSpace(reqBody))).Msg("HELLO message has body")
		vk.respondError(senderAddr, ErrBodyNotAccepted(mt.Hello).Error())
		return
	} // no need to check version here as we would normally reply according to packet rules, but we only support a single version

	// install requestor id in our pending table
	vk.pendingHellos.Store(reqHdr.ID, true, vk.pruneTime.hello)

	vk.respondSuccess(senderAddr,
		protocol.Header{
			Version: vk.versionSet.HighestSupported(), // at the moment, we only support a single version, so only reply with that version
			Type:    mt.HelloAck,
			ID:      vk.ID()},
		&pb.HelloAck{Height: uint32(vk.Height())})
}

// serveJoin answers JOIN packets with JOIN_ACCEPT or FAULT, installing the requestor as a child iff their height is appropriate.
func (vk *VaultKeeper) serveJoin(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) {
	if reqHdr.Shorthand {
		vk.respondError(senderAddr, ErrShorthandNotAccepted(mt.Hello).Error())
		return
	} else if len(reqBody) == 0 {
		vk.respondError(senderAddr, ErrBodyRequired(mt.Hello).Error())
		return
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		vk.respondError(senderAddr, ErrVersionNotSupported(reqHdr.Version).Error())
		return
	}

	// ensure that the requestor is in our hello table
	if found := vk.pendingHellos.Delete(reqHdr.ID); !found {
		vk.respondError(senderAddr, "must send HELLO first and join before it expires")
		return
	}

	// unpack the body
	var j = &pb.Join{}
	if err := proto.Unmarshal(reqBody, j); err != nil {
		vk.respondError(senderAddr, "failed to unmarshal body as a Join message")
		return
	}

	// acquire lock
	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()

	// handle as leaf or as cvk
	if j.IsVK {
		// validate rest of body
		if j.Height != uint32(vk.structure.height)-1 {
			vk.respondError(senderAddr, ErrBadHeightJoin(vk.structure.height, uint16(j.Height)).Error())
			return
		}
		addr, err := netip.ParseAddrPort(j.VkAddr)
		if err != nil {
			vk.respondError(senderAddr, ErrBadAddr(addr).Error())
			return
		}
		// check that this node is not already registered as a leaf
		// TODO
	} else {
		// check that this node is not already registered as cvk
		// TODO
	}

	vk.log.Info().Uint64("child ID", reqHdr.ID).Str("child VK address", j.VkAddr).Bool("vk?", j.IsVK).Msg("accepted JOIN")

	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(),
			Type: mt.JoinAccept,
			ID:   vk.ID(),
		}, &pb.JoinAccept{})

}

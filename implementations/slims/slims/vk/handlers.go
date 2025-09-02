package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"time"

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
func (vk *VaultKeeper) serveStatus(_ protocol.Header, reqBody []byte, senderAddr net.Addr) {
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
		vk.respondError(senderAddr, ErrShorthandNotAccepted(mt.Join).Error())
		return
	} else if len(reqBody) == 0 {
		vk.respondError(senderAddr, ErrBodyRequired(mt.Join).Error())
		return
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		vk.respondError(senderAddr, ErrVersionNotSupported(reqHdr.Version).Error())
		return
	}

	// unpack the body
	var j = &pb.Join{}
	if err := proto.Unmarshal(reqBody, j); err != nil {
		vk.respondError(senderAddr, "failed to unmarshal body as a JOIN message")
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
		// check if we can refresh a node with this ID
		if !vk.children.cvks.Refresh(reqHdr.ID, vk.pruneTime.cvk) {
			// ensure that the requestor is in our hello table
			if found := vk.pendingHellos.Delete(reqHdr.ID); !found {
				vk.respondError(senderAddr, "must send HELLO first and join before it expires")
				return
			}
			// install the new child
			if vk.addCVK(reqHdr.ID, addr) {
				vk.respondError(senderAddr, fmt.Sprintf("ID %d is already in use by a leaf", reqHdr.ID))
				return
			}
		}

	} else {
		// ensure that the requestor is in our hello table
		if found := vk.pendingHellos.Delete(reqHdr.ID); !found {
			vk.respondError(senderAddr, "must send HELLO first and join before it expires")
			return
		}
		if isCVK := vk.addLeaf(reqHdr.ID); isCVK {
			vk.respondError(senderAddr, fmt.Sprintf("ID %d is already in use by child vk", reqHdr.ID))
			return
		}
	}

	vk.log.Info().Uint64("child ID", reqHdr.ID).Str("child VK address", j.VkAddr).Bool("vk?", j.IsVK).Msg("accepted JOIN")

	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(),
			Type: mt.JoinAccept,
			ID:   vk.ID(),
		}, &pb.JoinAccept{Height: uint32(vk.structure.height)})

}

func (vk *VaultKeeper) serveRegister(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) {
	// validate parameters
	if reqHdr.Shorthand {
		vk.respondError(senderAddr, ErrShorthandNotAccepted(mt.Hello).Error())
		return
	} else if len(reqBody) == 0 {
		vk.respondError(senderAddr, ErrBodyRequired(mt.Register).Error())
		return
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		vk.respondError(senderAddr, ErrVersionNotSupported(reqHdr.Version).Error())
		return
	}
	// parse and validate body
	var registerReq pb.Register
	if err := proto.Unmarshal(reqBody, &registerReq); err != nil {
		vk.respondError(senderAddr, "failed to unmarshal body as a REGISTER message: "+err.Error())
		return
	} else if registerReq.Service == "" {
		vk.respondError(senderAddr, "service name cannot be empty")
		return
	}
	serviceStaleTime, err := time.ParseDuration(registerReq.Stale)
	if err != nil {
		vk.respondError(senderAddr, "bad stale time: "+err.Error())
		return
	}
	serviceAddress, err := netip.ParseAddrPort(registerReq.Address)
	if err != nil {
		vk.respondError(senderAddr, "bad service address: "+err.Error())
		return
	}
	// register the service
	if err := vk.addService(reqHdr.ID, registerReq.Service, serviceAddress, serviceStaleTime); err != nil {
		vk.respondError(senderAddr, err.Error())
		return
	}

	// respond with acceptance
	vk.respondSuccess(senderAddr, protocol.Header{
		Version: vk.versionSet.HighestSupported(),
		Type:    mt.RegisterAccept,
		ID:      vk.id,
	}, &pb.RegisterAccept{Service: registerReq.Service})

	// propagate the REGISTER up the vault
	// TODO

}

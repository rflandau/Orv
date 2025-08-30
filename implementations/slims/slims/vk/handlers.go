package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"net"

	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
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
		vk.respondError(senderAddr, ErrBodyNotAccepted(mt.Hello).Error())
		return
	} else if len(reqBody) != 0 {
		vk.log.Warn().Int("body length", len(reqBody)).Str("body", string(bytes.TrimSpace(reqBody))).Msg("HELLO message has body")
		vk.respondError(senderAddr, ErrBodyNotAccepted(mt.Hello).Error())
		return
	}

	// check the version to determine what version to respond with.
	// this redundant at the moment as we only support a single version.
	// TODO

	// install requestor id in our pending table
	vk.pendingHellos.Store(reqHdr.ID, true, DefaultHelloPruneTime)

	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(), Type: mt.HelloAck, ID: vk.ID()},
		&pb.HelloAck{Height: uint32(vk.Height())})
}

// serveHello answers HELLO packets by inserting the requestor into the serveHello table.
/*func (vk *VaultKeeper) serveHello(reqHdr protocol.Header, req *mux.Message, respWriter mux.ResponseWriter) {
	// unpack the body
	var bd bytes.Buffer
	if _, err := io.Copy(&bd, req.Body()); err != nil {
		vk.respondError(respWriter, codes.InternalServerError, err.Error())
		return
	}

	var pbReq pb.Hello
	if err := proto.Unmarshal(bd.Bytes(), &pbReq); err != nil {
		vk.respondError(respWriter, codes.BadRequest, err.Error())
		return
	}

	vk.log.Debug().Uint64("requestor id", pbReq.Id).Str("type", mt.Hello.String()).Send()

	// store/update the hello
	vk.pendingHellos.Store(pbReq.Id, true, helloPruneTime)

	// compose the body
	// TODO

	// set the header and respond
	vk.respondSuccess(respWriter, codes.Created,
		protocol.Header{
			Version: protocol.HighestSupported,
			Type:    mt.HelloAck,
		},
		nil)
}*/

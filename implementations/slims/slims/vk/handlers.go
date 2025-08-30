package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"net"

	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"google.golang.org/protobuf/proto"
)

//const helloPruneTime time.Duration = 3 * time.Second

// handler is the core processing called for each request.
// When a request arrives, it is logged and the Orv header is deserialized from it.
// Version is validated, then the request is passed to the appropriate subhandler.

// serveStatus answers STATUS packets by serializing most of the data in vk as json.
// Holds a read lock on structure.
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
	st := pb.StatusResp{
		Id:                vk.id,
		Height:            uint32(vk.structure.height),
		VersionsSupported: protocol.VersionsSupportedAsBytes(),
	}
	vk.structure.mu.RUnlock()

	// serialize via protobuf
	b, err := proto.Marshal(&st)
	if err != nil {
		vk.respondError(senderAddr, err.Error())
		return
	}

	vk.respondSuccess(senderAddr,
		&protocol.Header{Version: protocol.HighestSupported, Type: mt.StatusResp, ID: vk.id},
		b)
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

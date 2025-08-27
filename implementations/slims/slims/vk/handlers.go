package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"google.golang.org/protobuf/proto"
)

const helloPruneTime time.Duration = 3 * time.Second

// handler is the core processing called for each request.
// When a request arrives, it is logged and the Orv header is deserialized from it.
// Version is validated, then the request is passed to the appropriate subhandler.
func (vk *VaultKeeper) handler(resp mux.ResponseWriter, req *mux.Message) {
	// attempt to fetch an Orv header
	reqHdr := protocol.Header{}
	{
		if err := reqHdr.Deserialize(req.Body()); err != nil {
			vk.log.Error().Err(err).Msg("failed to deserialize header")
			vk.respondError(resp, codes.BadRequest, "failed to read header: "+err.Error())
			return
		}
		vk.log.Debug().Func(reqHdr.Zerolog).Str("token", req.Token().String()).Send()

	}
	// check that we support the requested version
	if !protocol.IsVersionSupported(reqHdr.Version) {
		vk.respondError(resp, codes.NotAcceptable, "unsupported version")
		return
	}

	// switch on request type.
	// Each sub-handler is expected to respond on its own.
	switch reqHdr.Type {
	// client requests that do not require a handshake
	case mt.Status:
		vk.serveStatus(reqHdr, req, resp)
	case mt.Hello:
		vk.serveHello(reqHdr, req, resp)
	// TODO ...
	default: // non-enumerated type or UNKNOWN
		vk.respondError(resp, codes.BadRequest, "message type must be set")
		return
	}

	// length-check the payload body
	/*payloadLen := len(respBody)
	if payloadLen > int(proto.MaxPayloadLength) {
		vk.log.Warn().
			Str("request type", reqHdr.Type.String()).
			Int("body size", payloadLen).
			Int("payload length limit", int(proto.MaxPayloadLength)).
			Msg("response body exceeds payload length")
		vk.respondError(resp, codes.InternalServerError, "response body exceeded max payload length")
		return
	}

	// generate the header
	respHdr := proto.Header{
		Version:       proto.HighestSupported,
		HopLimit:      1,
		PayloadLength: uint16(payloadLen),
	}
	respHdrB, err := respHdr.Serialize()
	if err != nil {
		vk.log.Warn().Str("request type", proto.MessageTypeString(reqHdr.Type)).Int("body size", payloadLen).Int("payload length limit", int(proto.MaxPayloadLength)).Msg("response body exceeds payload length")
		vk.respondError()
	}
	// write back the header and body
	if err := resp.SetResponse(respCode, orv.ResponseMediaType(), bytes.NewReader(append(respHdrB, respBody...))); err != nil {
		vk.log.Error().Err(err).Msg("failed to respond successfully")
		vk.respondError(resp, codes.InternalServerError, err.Error())
	}*/
}

// serveStatus answers STATUS packets by serializing most of the data in vk as json.
// Holds a read lock on structure.
func (vk *VaultKeeper) serveStatus(reqHdr protocol.Header, req *mux.Message, respWriter mux.ResponseWriter) {
	// drain the rest of the body
	var (
		drained   = make([]byte, 1024)
		totalRead int
	)
	for {
		n, err := req.Body().Read(drained)
		totalRead += n
		if errors.Is(err, io.EOF) {
			break
		}
		vk.log.Debug().Str("buffer", string(drained)).Int("total read", totalRead).Msg("drained body buffer")
	}

	// ensure we were not given a payload
	if reqHdr.PayloadLength != 0 || totalRead != 0 {
		vk.respondError(respWriter, codes.BadRequest,
			"STATUS does not accept a payload"+
				"(read "+strconv.FormatInt(int64(totalRead), 10)+" bytes with a declared payload length of "+strconv.FormatUint(uint64(reqHdr.PayloadLength), 10)+")")
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
		vk.respondError(respWriter, codes.InternalServerError, err.Error())
		return
	}
	if len(b) > int(protocol.MaxPayloadLength) {
		vk.respondError(respWriter, codes.InternalServerError, "payload exceeded max length")
		return
	}

	vk.respondSuccess(respWriter,
		codes.Content, // ! Content is defined only to work with GETs, but this otherwise fits the definition
		protocol.Header{Version: protocol.HighestSupported, PayloadLength: uint16(len(b)), Type: mt.StatusResp},
		b)
}

// serveHello answers HELLO packets by inserting the requestor into the serveHello table.
func (vk *VaultKeeper) serveHello(reqHdr protocol.Header, req *mux.Message, respWriter mux.ResponseWriter) {
	if reqHdr.PayloadLength == 0 {
		vk.respondError(respWriter, codes.BadRequest, "HELLO requires a payload")
	}

	// unpack the body
	var bd bytes.Buffer
	if n, err := io.Copy(&bd, req.Body()); err != nil {
		vk.respondError(respWriter, codes.InternalServerError, err.Error())
		return
	} else if n != int64(reqHdr.PayloadLength) {
		vk.respondError(respWriter, codes.BadRequest, "declared payload length ("+strconv.FormatUint(uint64(reqHdr.PayloadLength), 10)+") did not match actual payload ("+strconv.FormatInt(n, 10)+")")
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
	var body []byte
	// TODO

	// set the header and respond
	vk.respondSuccess(respWriter, codes.Created,
		protocol.Header{
			Version:       protocol.HighestSupported,
			Type:          mt.HelloAck,
			PayloadLength: uint16(len(body)),
		},
		nil)
}

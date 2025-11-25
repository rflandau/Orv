package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"math"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"google.golang.org/protobuf/proto"
)

// handler is the core processing called for each request.
// When a request arrives, it is logged and the Orv header is deserialized from it.
// Version is validated, then the request is passed to the appropriate subhandler.

const (
	registerPropagateTimeout time.Duration = 1 * time.Second
	// amount of time to consider the same token to be a duplicate (and thus dropped instead of handled).
	tokenCooldown time.Duration = 500 * time.Millisecond
)

//#region client request handling

// serveStatus answers STATUS packets, returning a variety of information about the vk.
// Functionally just calls Snapshot().
func (vk *VaultKeeper) serveStatus(_ protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// no header validation is required

	// check that we were not given a body
	if len(reqBody) != 0 {
		vk.log.Warn().Int("body length", len(reqBody)).Str("body", string(bytes.TrimSpace(reqBody))).Msg("STATUS message has body")
		return true, pb.Fault_BODY_NOT_ACCEPTED, nil
	}

	snap := vk.Snapshot()
	// translate into a pb-compliant struct
	var (
		pAddr             = snap.ParentAddr.String()
		ptHello           = snap.PruneTimes.Hello.String()
		ptServicelessLeaf = snap.PruneTimes.ServicelessLeaf.String()
		ptChildVK         = snap.PruneTimes.ChildVK.String()
		hbFreq            = snap.AutoHeartbeater.Frequency.String()
		hbLimit           = uint32(snap.AutoHeartbeater.Limit)
		cvks              = make(map[uint64]string)
		leaves            = slices.Collect(maps.Keys(snap.Children.Leaves))
		services          = make(map[string]uint32) // service name -> provider count
	)
	for cvkID, info := range snap.Children.CVKs {
		cvks[cvkID] = info.Addr.String()
		for service := range info.Services {
			services[service] += 1
		}
	}
	for _, leafServices := range snap.Children.Leaves {
		for service := range leafServices {
			services[service] += 1
		}
	}

	var st = &pb.StatusResp{
		Id:                        snap.ID,
		Addr:                      snap.Addr.String(),
		VersionsSupported:         snap.Versions.AsBytes(),
		Height:                    uint32(snap.Height),
		ParentId:                  &snap.ParentID,
		ParentAddr:                &pAddr,
		PruneTimesHello:           &ptHello,
		PruneTimesServicelessLeaf: &ptServicelessLeaf,
		PruneTimesChildVk:         &ptChildVK,
		ChildVks:                  cvks,
		ChildLeaves:               leaves,
		Services:                  services,
		AutoHbEnabled:             &snap.AutoHeartbeater.Enabled,
		AutoHbFrequency:           &hbFreq,
		AutoHbBadLimit:            &hbLimit,
	}
	// try to keep the message size below max size
	// (-10 to account for the size of the Orv header)
	if uint(proto.Size(st)) > uint(slims.MaxPacketSize)-10 { // second, drop cvks
		vk.log.Info().Uint("max body size", uint(slims.MaxPacketSize)-10).Uint("body size prior to reduction", uint(proto.Size(st))).Msg("trimming services out of status message to reduce size")
		st.Services = nil
	}
	if uint(proto.Size(st)) > uint(slims.MaxPacketSize)-10 { // first, drop leaves
		vk.log.Info().Uint("max body size", uint(slims.MaxPacketSize)-10).Uint("body size prior to reduction", uint(proto.Size(st))).Msg("trimming leaves out of status message to reduce size")
		st.ChildLeaves = nil
	}
	if uint(proto.Size(st)) > uint(slims.MaxPacketSize)-10 { // second, drop cvks
		vk.log.Info().Uint("max body size", uint(slims.MaxPacketSize)-10).Uint("body size prior to reduction", uint(proto.Size(st))).Msg("trimming cVKs out of status message to reduce size")
		st.ChildVks = nil
	}
	if uint(proto.Size(st)) > uint(slims.MaxPacketSize)-10 { // second, drop cvks
		vk.log.Info().Uint("max body size", uint(slims.MaxPacketSize)-10).Uint("body size prior to reduction", uint(proto.Size(st))).Msg("trimming parent info out of status message to reduce size")
		st.ParentId = nil
		st.ParentAddr = nil
	}
	// we'll readdress this later if need be

	// send data to client
	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(), Type: pb.MessageType_STATUS_RESP, ID: vk.id},
		st)
	return
}

// serveList answers LIST packets. Responds to any node (shorthand acceptable).
func (vk *VaultKeeper) serveList(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}

	// check that we were given a body
	if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, nil
	}

	var (
		req          pb.List
		addrToRespTo net.Addr
	)
	if err := pbun.Unmarshal(reqBody, &req); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	} else if req.Token == "" {
		return true, pb.Fault_BAD_TOKEN, nil
	} else if addrToRespTo, err = net.ResolveUDPAddr("udp", req.ResponseAddr); err != nil {
		return true, pb.Fault_MALFORMED_ADDRESS, []string{"failed to parse ResponseAddr"}
	}
	// send back an ACK
	vk.sendClientAck(senderAddr, req.Token, pb.MessageType_LIST_ACK)
	// mark this token as handled
	if swapped := vk.closedListTokens.CompareAndSwap(req.Token, false, true, tokenCooldown); !swapped {
		// duplicate of a request we have already handled
		// assume they just didn't get the ACK and do nothing
		return
	}
	if req.HopCount > 0 {
		req.HopCount -= 1
	}
	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()
	if req.HopCount <= 0 || !vk.structure.parentAddr.IsValid() { // answer if we are root or no more hops are allowed
		vk.sendListResponse(addrToRespTo, &req, reqHdr)
		return
	}
	// forward the request up the tree
	// if an error occurs, log it and answer as if we are the final destination
	// generate a dialer
	UDPParentAddr := net.UDPAddrFromAddrPort(vk.structure.parentAddr)
	conn, err := net.DialUDP("udp", nil, UDPParentAddr)
	if err != nil {
		vk.sendListResponse(addrToRespTo, &req, reqHdr)
		newCount := vk.hb.badHeartbeatCount.Add(1)
		vk.log.Warn().Str("action", "WritePacket").Err(err).Uint32("bad heartbeat count", newCount).Msg("failed to forward list request to parent")
		return
	}

	// forward the request up the chain
	if _, err := protocol.WritePacket(context.Background(), conn, protocol.Header{
		Version: reqHdr.Version,
		Type:    pb.MessageType_LIST,
		ID:      vk.id,
	}, &req); err != nil {
		vk.sendListResponse(addrToRespTo, &req, reqHdr)
		newCount := vk.hb.badHeartbeatCount.Add(1)
		vk.log.Warn().Str("action", "WritePacket").Err(err).Uint32("bad heartbeat count", newCount).Msg("failed to forward list request to parent")
		return
	}
	vk.log.Info().Str("token", req.Token).Uint32("decremented hop count", req.HopCount).Str("response address", req.ResponseAddr).Msg("forwarded LIST request to parent")
	// because we don't care about repeating, we don't actually care if we get an ACK.
	return
}

// helper function for serveList.
// Sends a LIST_RESPONSE to the given address and log it.
func (vk *VaultKeeper) sendListResponse(parsedRespAddr net.Addr, req *pb.List, reqHdr protocol.Header) {
	vk.children.mu.Lock()
	var resp = pb.ListResp{
		Token:    req.Token,
		Services: slices.Collect(maps.Keys(vk.children.allServices)),
	}
	vk.children.mu.Unlock()
	vk.respondSuccess(parsedRespAddr, protocol.Header{Version: reqHdr.Version, Type: pb.MessageType_LIST_RESP, ID: vk.id}, &resp)
	vk.log.Info().Str("token", req.Token).Uint32("decremented hop count", req.HopCount).Str("response address", parsedRespAddr.String()).Msg("answered LIST request")
}

func (vk *VaultKeeper) serveGet(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// validate parameters
	if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}
	// check that we were given a body
	if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, nil
	}

	var (
		req          pb.Get
		addrToRespTo net.Addr
	)
	if err := pbun.Unmarshal(reqBody, &req); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	} else if req.Token == "" {
		return true, pb.Fault_BAD_TOKEN, nil
	} else if addrToRespTo, err = net.ResolveUDPAddr("udp", req.ResponseAddr); err != nil {
		return true, pb.Fault_MALFORMED_ADDRESS, []string{"failed to parse ResponseAddr"}
	}
	// send back an ACK
	vk.sendClientAck(senderAddr, req.GetToken(), pb.MessageType_GET_ACK)
	// mark this token as handled
	// TODO
	if req.HopLimit > 0 {
		req.HopLimit -= 1
	}

	// check if we know of this service
	vk.children.mu.Lock()
	providers, found := vk.children.allServices[req.Service]
	if found {
		// pick any provider to answer with
		for _, k := range providers {
			// answer the request
			vk.respondSuccess(addrToRespTo, protocol.Header{
				Version: reqHdr.Version,
				Type:    pb.MessageType_GET_RESP,
				ID:      vk.id,
			}, &pb.GetResp{
				Token:   req.Token,
				Service: req.Service,
				Address: k.String(),
			})
			break
		}
		vk.children.mu.Unlock()
		return
	}
	vk.children.mu.Unlock()

	// not found
	vk.structure.mu.RLock()
	defer vk.structure.mu.RUnlock()
	if req.HopLimit <= 0 || !vk.structure.parentAddr.IsValid() { // no more hops are allowed or we are root, but we did not find the service
		vk.respondSuccess(addrToRespTo, protocol.Header{
			Version: reqHdr.Version,
			Type:    pb.MessageType_GET_RESP,
			ID:      vk.id,
		}, &pb.GetResp{
			Token:   req.Token,
			Service: req.Service,
			Address: "",
		})
		return
	}
	// forward the request up the tree
	if _, _, err := vk.messageParent(pb.MessageType_GET, &req); err != nil {
		vk.log.Warn().Err(err).Uint32("decremented hop limit", req.HopLimit).Msg("failed to forward GET request up the tree")
	} // NOTE(rlandau): does not validate the returned ACK
	return
}

// helper function for serveList and serveGet.
// panics if typ is not GET_ACK or LIST_ACK.
func (vk *VaultKeeper) sendClientAck(senderAddr net.Addr, token string, typ pb.MessageType) {
	var (
		body proto.Message
	)
	switch typ {
	case pb.MessageType_GET_ACK:
		body = &pb.GetAck{Token: token}
	case pb.MessageType_LIST_ACK:
		body = &pb.ListAck{Token: token}
	default:
		panic("unhandled message type")
	}

	vk.respondSuccess(senderAddr,
		protocol.Header{
			Version: vk.versionSet.HighestSupported(),
			Type:    typ,
			ID:      vk.ID(),
		},
		body,
	)
	vk.log.Info().Str("token", token).Str("sender address", senderAddr.String()).Str("request type", typ.String()).Msg("acknowledged client request")
}

//#endregion client request handling

// serveHello answers HELLO packets with HELLO_ACK or FAULT.
// Selects a version based on the request version (the version in the header) and what versions we support.
func (vk *VaultKeeper) serveHello(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// validate parameters
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) != 0 {
		vk.log.Warn().Int("body length", len(reqBody)).Str("body", string(bytes.TrimSpace(reqBody))).Msg("HELLO message has body")
		return true, pb.Fault_BODY_NOT_ACCEPTED, nil
	} // no need to check version here as we would normally reply according to packet rules, but we only support a single version

	// install requestor id in our pending table
	vk.pendingHellos.Store(reqHdr.ID, true, vk.pruneTime.Hello)

	vk.respondSuccess(senderAddr,
		protocol.Header{
			Version: vk.versionSet.HighestSupported(), // at the moment, we only support a single version, so only reply with that version
			Type:    pb.MessageType_HELLO_ACK,
			ID:      vk.ID()},
		&pb.HelloAck{Height: uint32(vk.Height())})

	return
}

// serveJoin answers JOIN packets with JOIN_ACCEPT or FAULT, installing the requestor as a child iff their height is appropriate.
func (vk *VaultKeeper) serveJoin(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}

	// unpack the body
	var j = &pb.Join{}
	if err := pbun.Unmarshal(reqBody, j); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	}

	// acquire lock
	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()

	// handle as leaf or as cvk
	if j.IsVk {
		// validate rest of body
		if j.Height != uint32(vk.structure.height)-1 {
			return true, pb.Fault_BAD_HEIGHT, nil //[]string{fmt.Sprintf("to join as a child VK to this VK, height (%d) must equal parent VK height (%d)-1", j.Height, vk.Height())}
		}
		addr, err := netip.ParseAddrPort(j.VkAddr)
		if err != nil {
			return true, pb.Fault_MALFORMED_ADDRESS, nil
		}
		// check if we can refresh a node with this ID
		if !vk.children.cvks.Refresh(reqHdr.ID, vk.pruneTime.ChildVK) {
			// ensure that the requestor is in our hello table
			if found := vk.pendingHellos.Delete(reqHdr.ID); !found {
				return true, pb.Fault_HELLO_REQUIRED, nil
			}
			// install the new child
			if vk.addCVK(reqHdr.ID, addr) {
				return true, pb.Fault_ID_IN_USE, nil
			}
		}

	} else {
		// ensure that the requestor is in our hello table
		if found := vk.pendingHellos.Delete(reqHdr.ID); !found {
			return true, pb.Fault_HELLO_REQUIRED, nil
		}
		if isCVK := vk.addLeaf(reqHdr.ID); isCVK {
			return true, pb.Fault_ID_IN_USE, nil
		}
	}

	vk.log.Info().Uint64("child ID", reqHdr.ID).Str("child VK address", j.VkAddr).Bool("vk?", j.IsVk).Msg("accepted JOIN")

	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(),
			Type: pb.MessageType_JOIN_ACCEPT,
			ID:   vk.ID(),
		}, &pb.JoinAccept{Height: uint32(vk.structure.height)})

	return
}

// serveMerge handles incoming merge requests.
// Handling mostly revolves around ensuring the requestor's height is equal to ours.
func (vk *VaultKeeper) serveMerge(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// NOTE: because we are using protobufs, a zero-height request will cause an empty body.
	// Hence, we cannot check for an empty body here even though we require a height.
	// If no body is given, use zero height.
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}
	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()
	if vk.structure.parentID == reqHdr.ID { // if this request is coming from our parent, we can assume our prior acceptance never arrived
		// sanity check the height
		var reqHeight uint32
		if len(reqBody) != 0 {
			var m pb.Merge
			if err := pbun.Unmarshal(reqBody, &m); err != nil {
				return true, pb.Fault_MALFORMED_BODY, []string{err.Error()}
			}
			reqHeight = m.Height
		}

		// if this is a duplicate request, our parent's height should be equal to ours (as they would not have increment yet and we don't need to).
		if reqHeight != uint32(vk.structure.height) {
			vk.log.Warn().Uint16("current height", vk.structure.height).
				Uint32("received height", reqHeight).
				Msg("received MERGE with mismatching height from pre-existing parent. Dropping parent...")
			vk.structure.parentAddr = netip.AddrPort{}
			vk.structure.parentID = 0
			return true, pb.Fault_BAD_HEIGHT, []string{
				fmt.Sprintf("duplicate merge request from existing parent has bad height. Expected %v, got %v.", vk.structure.height, reqHeight),
				"Dropped as parent.",
			}
		}
		// re-send acceptance
		vk.respondSuccess(senderAddr,
			protocol.Header{
				Version: vk.versionSet.HighestSupported(),
				Type:    pb.MessageType_MERGE_ACCEPT,
				ID:      vk.id,
			},
			&pb.MergeAccept{
				Height: uint32(vk.structure.height), // no need to re-increment
			},
		)
		return
	} else if vk.structure.parentAddr.IsValid() { // we already have a different parent!
		return true, pb.Fault_NOT_ROOT, nil
	}

	// check that this VK is in our hello table
	if found := vk.pendingHellos.Delete(reqHdr.ID); !found {
		return true, pb.Fault_HELLO_REQUIRED, nil
	}

	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()
	// check that we do not have a child with this ID already
	if _, found := vk.children.leaves[reqHdr.ID]; found {
		return true, pb.Fault_ID_IN_USE, []string{"given ID is already claimed by a known leaf"}
	}
	if _, found := vk.children.cvks.Load(reqHdr.ID); found {
		return true, pb.Fault_ID_IN_USE, []string{"given ID is already claimed by a known child VK"}
	}

	// check the height of the requestor
	var reqHeight uint32
	if len(reqBody) != 0 {
		var m pb.Merge
		if err := pbun.Unmarshal(reqBody, &m); err != nil {
			return true, pb.Fault_MALFORMED_BODY, []string{err.Error()}
		}
		reqHeight = m.Height
	}
	if reqHeight != uint32(vk.structure.height) {
		return true, pb.Fault_BAD_HEIGHT, []string{"merges can only occur between equal height nodes. My height is " + strconv.FormatUint(uint64(vk.structure.height), 10)}
	} else if reqHeight > math.MaxUint16-1 { // bounds check
		return true, pb.Fault_BAD_HEIGHT, []string{"height must be less than 65535"}
	}

	// update our parent
	if ap, err := netip.ParseAddrPort(senderAddr.String()); err != nil {
		vk.log.Info().Err(err).Str("senderAddr", senderAddr.String()).Msg("failed to accept merge: failed to parse new parent address from senderAddr")
		return true, pb.Fault_MALFORMED_ADDRESS, nil
	} else {
		vk.structure.parentAddr = ap
	}
	vk.structure.parentID = reqHdr.ID
	// accept the request
	vk.respondSuccess(senderAddr, protocol.Header{
		Version: vk.versionSet.HighestSupported(),
		Type:    pb.MessageType_MERGE_ACCEPT,
		ID:      vk.id},
		&pb.MergeAccept{Height: uint32(vk.structure.height)},
	)
	return
}

func (vk *VaultKeeper) serveIncrement(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_Errnos(pb.Fault_VERSION_NOT_SUPPORTED), nil
	}
	// ensure this request is coming from our parent
	vk.structure.mu.RLock()
	cParentAddr := vk.structure.parentAddr.Addr().String()
	cParentID := vk.structure.parentID
	vk.structure.mu.RUnlock()
	if cParentAddr != senderAddr.String() {
		vk.log.Warn().Msg("INCREMENT request received from non-parent")
		return true, pb.Fault_NOT_PARENT, nil
	}

	// unpack the body
	var inc pb.Increment
	if err := pbun.Unmarshal(reqBody, &inc); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	}
	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()
	// ensure parent hasn't changed between critical sections
	if cParentAddr != vk.structure.parentAddr.Addr().String() || cParentID != vk.structure.parentID {
		vk.log.Info().
			Str("cached parent address", cParentAddr).Uint64("cached parent ID", cParentID).
			Str("parent address", vk.structure.parentAddr.String()).Uint64("parent ID", vk.structure.parentID).Msg("parent changed between INCREMENT critical sections")
		return true, pb.Fault_NOT_PARENT, nil
	}
	// ensure the new height is exactly curHeight+1
	switch inc.NewHeight {
	case uint32(vk.structure.height): // duplicate
		// if our heights already match, we can consider this a duplicate and thus a no-op.
		// (given we have already validated that the sender is our parent).
		vk.respondSuccess(senderAddr,
			protocol.Header{
				Version: vk.versionSet.HighestSupported(),
				Type:    pb.MessageType_INCREMENT_ACK,
				ID:      vk.id,
			}, &pb.IncrementAck{NewHeight: uint32(vk.structure.height)})
		// NOTE(rlandau): as we already incremented our height, we assume we have already noticed our children as well
		return
	case uint32(vk.structure.height) + 1: // success
		wg := sync.WaitGroup{}

		// confirm the increment
		wg.Go(func() {
			vk.respondSuccess(senderAddr,
				protocol.Header{
					Version: vk.versionSet.HighestSupported(),
					Type:    pb.MessageType_INCREMENT_ACK,
					ID:      vk.id,
				}, &pb.IncrementAck{NewHeight: uint32(vk.structure.height)})
		})
		// propagate the INCREMENT down our branches
		vk.children.cvks.RangeLocked(func(id slims.NodeID, cvk struct {
			services map[string]netip.AddrPort
			addr     netip.AddrPort
		}) bool {
			wg.Go(func() {
				UDPAddr := net.UDPAddrFromAddrPort(cvk.addr)
				if UDPAddr == nil {
					vk.log.Error().Msgf("failed to convert netip.AddrPort (%v) to net.UDPAddr", cvk.addr)
					return
				}
				if err := vk.increment(UDPAddr); err != nil {
					vk.log.Warn().Msgf("%s", err)
				}
			})
			return true
		})
		// await
		wg.Wait()
		return // all done!
	default: // bad height
		vk.log.Warn().Msg("unexpected INCREMENT height")
		return true, pb.Fault_BAD_HEIGHT, nil
	}
}

// serveLeave handles incoming LEAVE packets.
// If the ID matches a known child, that child will be removed from the list of known children and all its services deregistered up the tree.
func (vk *VaultKeeper) serveLeave(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) != 0 {
		return true, pb.Fault_BODY_NOT_ACCEPTED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}

	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()

	// check if this child is a VK
	if found := vk.RemoveCVK(reqHdr.ID, false); found {
		vk.respondSuccess(senderAddr, protocol.Header{
			Version: vk.versionSet.HighestSupported(),
			Type:    pb.MessageType_LEAVE_ACK,
			ID:      vk.id,
		}, nil)
		return
	}
	// check if this child is a leaf
	if found := vk.RemoveLeaf(reqHdr.ID, false); found {
		vk.respondSuccess(senderAddr, protocol.Header{
			Version: vk.versionSet.HighestSupported(),
			Type:    pb.MessageType_LEAVE_ACK,
			ID:      vk.id,
		}, nil)
		return
	}

	// not found
	return true, pb.Fault_UNKNOWN_CHILD_ID, nil
}

func (vk *VaultKeeper) serveRegister(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// validate parameters
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}
	// parse and validate body
	var registerReq pb.Register
	if err := proto.Unmarshal(reqBody, &registerReq); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	} else if registerReq.Service == "" {
		return true, pb.Fault_BAD_SERVICE_NAME, []string{"service name cannot be empty"}
	}
	serviceStaleTime, err := time.ParseDuration(registerReq.Stale)
	if err != nil {
		return true, pb.Fault_BAD_STALE_TIME, nil
	}
	serviceAddress, err := netip.ParseAddrPort(registerReq.Address)
	if err != nil {
		return true, pb.Fault_MALFORMED_ADDRESS, []string{fmt.Sprintf("bad service address ('%s' @ %v)", registerReq.Service, registerReq.Address)}
	}
	// register the service
	if erred, code, ei := vk.addService(reqHdr.ID, registerReq.Service, serviceAddress, serviceStaleTime); erred {
		return erred, code, ei
	}

	// respond with acceptance
	vk.respondSuccess(senderAddr, protocol.Header{
		Version: vk.versionSet.HighestSupported(),
		Type:    pb.MessageType_REGISTER_ACCEPT,
		ID:      vk.id,
	}, &pb.RegisterAccept{Service: registerReq.Service})

	vk.structure.mu.Lock()
	if vk.structure.parentAddr.IsValid() {
		// register the service to our parent under our name
		ctx, cancel := context.WithTimeout(context.Background(), registerPropagateTimeout)
		defer cancel()
		if parentID, accept, err := client.Register(ctx, vk.id, vk.structure.parentAddr, registerReq.Service, serviceAddress, 0); err != nil {
			vk.log.Error().Err(err).Uint64("parent id", vk.structure.parentID).Msg("failed to register service with parent")
		} else {
			if parentID != vk.structure.parentID {
				vk.log.Warn().Uint64("expected parent id", vk.structure.parentID).Uint64("actual parent id", parentID).Msg("incorrect response ID from parent when registering service")
			} else if accept.Service != registerReq.Service {
				vk.log.Warn().Uint64("expected parent id", vk.structure.parentID).Uint64("actual parent id", parentID).Msg("incorrect response ID from parent when registering service")
			}
		}
		vk.log.Debug().AnErr("register error", err).Str("service", registerReq.Service).Str("service address", serviceAddress.String()).Msg("registered child service to parent")
	}
	vk.structure.mu.Unlock()
	return
}

func (vk *VaultKeeper) serveDeregister(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// validate parameters
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, []string{"body must include one field: \"Service\""}
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}
	// unpack the body
	msg := pb.Deregister{}
	if err := pbun.Unmarshal(reqBody, &msg); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	}
	// check that this is a known service
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()

	// remove this service from the list of providers
	if eno := vk.removeProvider(msg.Service, reqHdr.ID); eno != 0 {
		// set return states
		errored = true
		errno = eno
	}
	// also remove this service from the child
	if inf, found := vk.children.cvks.Load(reqHdr.ID); found { // check cvks
		delete(inf.services, msg.Service)
	} else if inf, found := vk.children.leaves[reqHdr.ID]; found { // check leaf
		delete(inf.services, msg.Service)
	} else {
		// we found the service in the providers view, but its owner does not exist.
		// Could be a timing thing, could be an indication of a broader issue
		vk.log.Warn().Str("service", msg.Service).Msg("failed to prune service from child view; child is not a known leaf or vk")
	}
	if !errored {
		vk.respondSuccess(senderAddr, protocol.Header{
			Version: vk.versionSet.HighestSupported(),
			Type:    pb.MessageType_DEREGISTER_ACK,
			ID:      vk.id,
		}, &pb.DeregisterAck{Service: msg.Service})
	}
	return errored, errno, nil
}

// serveServiceHeartbeat answers SERVICE_HEARTBEATs, refreshing all attached (known) services associated to the requestor's ID.
func (vk *VaultKeeper) serveServiceHeartbeat(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	//validate parameters
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) == 0 {
		return true, pb.Fault_BODY_REQUIRED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}
	//unpack the body
	msg := pb.ServiceHeartbeat{}
	if err := proto.Unmarshal(reqBody, &msg); err != nil {
		return true, pb.Fault_MALFORMED_BODY, nil
	}
	// check that this is a known leaf ID
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()
	s, found := vk.children.leaves[reqHdr.ID]
	if !found {
		return true, pb.Fault_UNKNOWN_CHILD_ID, nil
	}
	response := &pb.ServiceHeartbeatAck{
		Refresheds: make([]string, 0),
		Unknowns:   make([]string, 0),
	}
	// refresh each service given
	for _, svc := range msg.GetServices() {
		leafService, found := s.services[svc]
		if !found ||
			!leafService.pruner.Stop() { // had expired, but was not yet pruned; allow pruner to do its job
			response.Unknowns = append(response.Unknowns, svc)
			continue
		}
		leafService.pruner.Reset(leafService.stale)
		vk.log.Debug().Uint64("leaf ID", reqHdr.ID).Str("service", svc).Msg("refreshed service")
		response.Refresheds = append(response.Refresheds, svc)
	}
	// if no services were refreshed successfully, send back a fault
	if len(response.Refresheds) == 0 {
		return true, pb.Fault_ALL_UNKNOWN, append([]string{"Services:"}, response.Unknowns...)
	}
	vk.respondSuccess(senderAddr, protocol.Header{
		Version: vk.versionSet.HighestSupported(),
		Type:    pb.MessageType_SERVICE_HEARTBEAT_ACK,
		ID:      vk.ID(),
	}, response)

	return
}

// serveVKHeartbeat answers VK_HEARTBEATs, refreshing the associated child VK (if found).
func (vk *VaultKeeper) serveVKHeartbeat(reqHdr protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// validate parameters
	if reqHdr.Shorthand {
		return true, pb.Fault_SHORTHAND_NOT_ACCEPTED, nil
	} else if len(reqBody) != 0 {
		return true, pb.Fault_BODY_NOT_ACCEPTED, nil
	} else if !vk.versionSet.Supports(reqHdr.Version) {
		return true, pb.Fault_VERSION_NOT_SUPPORTED, nil
	}
	// check that this is a known cvk ID
	vk.children.mu.Lock()

	found := vk.children.cvks.Refresh(reqHdr.ID, vk.pruneTime.ChildVK)

	vk.children.mu.Unlock()
	if !found {
		return true, pb.Fault_UNKNOWN_CHILD_ID, nil
	}
	vk.log.Debug().Uint64("child ID", reqHdr.ID).Msg("refreshed child vk")
	vk.respondSuccess(senderAddr, protocol.Header{
		Version:   vk.versionSet.HighestSupported(),
		Shorthand: false,
		Type:      pb.MessageType_VK_HEARTBEAT_ACK,
		ID:        vk.id,
	}, nil)
	return
}

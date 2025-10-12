package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"time"

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

	var st = pb.StatusResp{
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

	// send data to client
	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(), Type: pb.MessageType_STATUS_RESP, ID: vk.id},
		&st)
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
	vk.sendListAck(senderAddr, req.Token)
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
// Sends a LIST_ACK to the given address.
func (vk *VaultKeeper) sendListAck(senderAddr net.Addr, token string) {
	var la = pb.ListAck{Token: token}

	vk.respondSuccess(senderAddr,
		protocol.Header{
			Version: vk.versionSet.HighestSupported(),
			Type:    pb.MessageType_LIST_ACK,
			ID:      vk.ID(),
		}, &la)
	vk.log.Info().Str("token", token).Str("sender address", senderAddr.String()).Msg("acknowledged LIST request")
}

// helper function for serveList.
// Sends a LIST_RESPONSE to the given address and log it.
func (vk *VaultKeeper) sendListResponse(parsedRespAddr net.Addr, req *pb.List, reqHdr protocol.Header) {
	vk.children.mu.Lock()
	var resp = pb.ListResponse{
		Token:    req.Token,
		Services: slices.Collect(maps.Keys(vk.children.allServices)),
	}
	vk.children.mu.Unlock()
	vk.respondSuccess(parsedRespAddr, protocol.Header{Version: reqHdr.Version, Type: pb.MessageType_LIST_RESP, ID: vk.id}, &resp)
	vk.log.Info().Str("token", req.Token).Uint32("decremented hop count", req.HopCount).Str("response address", parsedRespAddr.String()).Msg("answered LIST request")
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
			return true, pb.Fault_BAD_HEIGHT, []string{fmt.Sprintf("to join as a child VK to this VK, height (%d) must equal parent VK height (%d)-1", j.Height, vk.Height())}
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

package vaultkeeper

// handlers.go contains the switch on type for incoming packets and the subroutines invoked by each case/packet type.

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"google.golang.org/protobuf/proto"
)

// handler is the core processing called for each request.
// When a request arrives, it is logged and the Orv header is deserialized from it.
// Version is validated, then the request is passed to the appropriate subhandler.

const registerPropagateTimeout time.Duration = 1 * time.Second

// serveStatus answers STATUS packets, returning a variety of information about the vk.
// Briefly holds a read lock on structure.
func (vk *VaultKeeper) serveStatus(_ protocol.Header, reqBody []byte, senderAddr net.Addr) (errored bool, errno pb.Fault_Errnos, extraInfo []string) {
	// no header validation is required

	// check that we were not given a body
	if len(reqBody) != 0 {
		vk.log.Warn().Int("body length", len(reqBody)).Str("body", string(bytes.TrimSpace(reqBody))).Msg("STATUS message has body")
		return true, pb.Fault_BODY_NOT_ACCEPTED, nil
	}

	vk.structure.mu.RLock()
	// gather data
	st := &pb.StatusResp{ // TODO
		Height:            uint32(vk.structure.height),
		VersionsSupported: vk.versionSet.AsBytes(),
	}
	vk.structure.mu.RUnlock()

	// send data to client
	vk.respondSuccess(senderAddr,
		protocol.Header{Version: vk.versionSet.HighestSupported(), Type: pb.MessageType_STATUS_RESP, ID: vk.id},
		st)
	return
}

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
	} // TODO check for unknown fields

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
		return true, pb.Fault_MALFORMED_ADDRESS, []string{fmt.Sprintf("bad service address ('%s' @ )")}
	}
	// register the service
	if erred, code, ei := vk.addService(reqHdr.ID, registerReq.Service, serviceAddress, serviceStaleTime); err != nil {
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
		// TODO  test me
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

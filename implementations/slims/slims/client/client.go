// Package client provides static subroutines for interacting with a vault and vaultkeeper.
// Each subroutine lists if it is intended to be used only by leaves, only by vks, or by any node (even those not associated to the tree).
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidAddrPort = net.InvalidAddrError("target must be a valid address+port")

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
// Sends the packet as protocol.SupportedVersions().HighestSupported().
//
// This subroutine can be invoked by any node.
func Hello(ctx context.Context, myID slims.NodeID, target netip.AddrPort) (vkID slims.NodeID, vkVersion version.Version, _ *pb.HelloAck, err error) {
	// validate parameters
	if ctx == nil {
		return 0, version.Version{}, nil, slims.ErrNilCtx
	} else if !target.IsValid() {
		return 0, version.Version{}, nil, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, version.Version{}, nil, ErrInvalidAddrPort
	}
	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, version.Version{}, nil, err
	}
	// send
	if _, err := protocol.WritePacket(ctx, conn,
		protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Type: pb.MessageType_HELLO, ID: myID}, nil); err != nil {
		return 0, version.Version{}, nil, err
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, version.Version{}, nil, err
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return respHdr.ID, respHdr.Version, nil, err
		}
		return respHdr.ID, respHdr.Version, nil, slims.FormatFault(f)
	case pb.MessageType_HELLO_ACK:
		ha := &pb.HelloAck{}
		if err := proto.Unmarshal(respBody, ha); err != nil {
			return respHdr.ID, respHdr.Version, nil, err
		}
		return respHdr.ID, respHdr.Version, ha, nil
	default:
		return respHdr.ID, respHdr.Version, nil, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// JoinInfo contains information about the requestor used to compose the JOIN request.
// If you are trying to join as a vk, prefer vaultkeeper.Join() (which will populate this for you).
type JoinInfo struct {
	IsVK   bool           // am I a VK?
	VKAddr netip.AddrPort // required iff isVK: where I am listening
	Height uint16         // required iff isVK: what is my current height
}

// Join sends a JOIN packet to the given address, returning the target node's response or an error.
// Sends the packet as protocol.SupportedVersions().HighestSupported().
// If you are trying to join as a vk, prefer vaultkeeper.Join().
//
// This subroutine can be invoked by any node wishing to join a vault (as a leaf or as a child vk).
func Join(ctx context.Context, myID slims.NodeID, target netip.AddrPort, req JoinInfo) (vkID slims.NodeID, _ *pb.JoinAccept, _ error) {
	// validate parameters
	if ctx == nil {
		return 0, nil, slims.ErrNilCtx
	} else if !target.IsValid() {
		return 0, nil, ErrInvalidAddrPort
	} else if req.IsVK && !req.VKAddr.IsValid() {
		return 0, nil, errors.New("requestor's vk address is required if isVK")
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, nil, ErrInvalidAddrPort
	}

	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, nil, err
	}
	// send
	if _, err := protocol.WritePacket(ctx, conn,
		protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Type: pb.MessageType_JOIN, ID: myID},
		&pb.Join{IsVk: req.IsVK, VkAddr: req.VKAddr.String(), Height: uint32(req.Height)}); err != nil {
		return 0, nil, err
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, nil, err
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, nil, slims.FormatFault(f)
	case pb.MessageType_JOIN_ACCEPT:
		accept := &pb.JoinAccept{}
		if err := proto.Unmarshal(respBody, accept); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, accept, nil
	default:
		return respHdr.ID, nil, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// Register sends a REGISTER packet to the given address, returning the target node's ID and response.
// This registers a single service with the target so long as myID is a known children.
func Register(ctx context.Context, myID slims.NodeID, target netip.AddrPort, service string, serviceAddr netip.AddrPort, stale time.Duration) (vkID slims.NodeID, _ *pb.RegisterAccept, _ error) {
	// validate parameters
	if ctx == nil {
		return 0, nil, slims.ErrNilCtx
	} else if !target.IsValid() {
		return 0, nil, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return //0, nil, ErrInvalidAddrPort
	}

	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, nil, err
	}
	if _, err := protocol.WritePacket(ctx, conn,
		protocol.Header{
			Version: protocol.SupportedVersions().HighestSupported(),
			Type:    pb.MessageType_REGISTER,
			ID:      myID,
		},
		&pb.Register{
			Service: service,
			Address: serviceAddr.String(),
			Stale:   stale.String(),
		}); err != nil {
		return 0, nil, err
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, nil, err
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, nil, slims.FormatFault(f)
	case pb.MessageType_REGISTER_ACCEPT:
		accept := &pb.RegisterAccept{}
		if err := proto.Unmarshal(respBody, accept); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, accept, nil
	default:
		return respHdr.ID, nil, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// ServiceHeartbeat sends a SERVICE_HEARTBEAT packet to the given address, which must be owned by this node's parent.
// This should be run in a loop to ensure the service does not get pruned.
// Because we are using UDP and thus packets can get lost, you should repeat this if no ACK is received (otherwise the service risks being pruned).
//
// ServicesRefreshed should be checked to ensure it matches the given list of services; missing services may or may not have been refreshed.
//
// Do NOT use this for VK heartbeats. Use vk.Heartbeat() for that (or rely on the automated heartbeating this library implements in VKs).
// TODO write unit tests and a vk handler for me!
func ServiceHeartbeat(ctx context.Context, myID slims.NodeID, parentAddr netip.AddrPort, services []string) (vkID slims.NodeID, servicesRefreshed []string, servicesUnknown []string, rerr error) {
	if ctx == nil {
		rerr = slims.ErrNilCtx
		return
	} else if !parentAddr.IsValid() {
		rerr = ErrInvalidAddrPort
		return
	} else if len(services) == 0 { // job's done
		rerr = ErrInvalidAddrPort
		return
	}
	UDPAddr := net.UDPAddrFromAddrPort(parentAddr)
	if UDPAddr == nil {
		rerr = ErrInvalidAddrPort
		return
	}
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		rerr = err
		return
	}
	// send the packet, but length check it first (assuming no compression)
	sh := &pb.ServiceHeartbeat{Services: services}

	if int(protocol.LongHeaderLen)+proto.Size(sh) > int(slims.MaxPacketSize) {
		rerr = errors.New("packet size is too large for max packet buffer")
		return
	}

	if _, err := protocol.WritePacket(ctx, conn,
		protocol.Header{
			Version: protocol.SupportedVersions().HighestSupported(),
			Type:    pb.MessageType_SERVICE_HEARTBEAT,
			ID:      myID,
		}, sh); err != nil {
		rerr = err
		return
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		rerr = err
		return
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return respHdr.ID, nil, nil, err
		}
		return respHdr.ID, nil, nil, slims.FormatFault(f)
	case pb.MessageType_SERVICE_HEARTBEAT_ACK:
		ack := &pb.ServiceHeartbeatAck{}
		if err := proto.Unmarshal(respBody, ack); err != nil {
			return respHdr.ID, nil, nil, err
		}
		return respHdr.ID, ack.Refresheds, ack.Unknowns, nil
	default:
		return respHdr.ID, nil, nil, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// ParentInfo can be used to pass validate-able parent info to client requests.
type ParentInfo struct {
	ID   slims.NodeID
	Addr netip.AddrPort
}

// AutoServiceHeartbeat spins out a goroutine to send heartbeats for services on behalf of myID every frequency.
// Easy way to spin up heartbeating for your service(s) without having to manually incorporate ServiceHeartbeats.
//
// hbWriteTimeout sets the timeout for each HB send.
//
// frequency sets how often a service heartbeat (containing all valid services) will be sent.
//
// hbErrs, if given, will contain errors that occur as a result of the ServiceHeartbeat calls.
// If nil, the errors will be thrown away.
// Caller is responsible for closing hbErrs, but should only do so after calling cancel.
//
// If p.ID does not equal the ID returned by ServiceHeartbeat, the goroutine will send an error and die.
//
// If a service is returned as unknown (it expired or was otherwise removed from the parent, thus failing to heartbeat), it will be removed from the list of services to heartbeat.
// If no services remain to be heartbeated for, the goroutine will exit.
func AutoServiceHeartbeat(hbWriteTimeout, frequency time.Duration, myID slims.NodeID, p ParentInfo, servicesToHB []string, hbErrs chan<- error) (cancel func(), _ error) {
	// validate params
	if frequency <= 0 {
		return nil, errors.New("frequency must be a positive duration")
	} else if !p.Addr.IsValid() {
		return nil, ErrInvalidAddrPort
	}

	var (
		done atomic.Bool // tracks if this autoheartber been cancelled or is otherwise done
		cnl  = make(chan bool)
	)
	services := slices.Clone(servicesToHB) // copy so we can safely alter list
	go func() {
		for len(services) > 0 {
			select {
			case <-cnl: // die on cancel
				return
			case <-time.After(frequency): // send HB on freq
				// send the request
				ctx, c := context.WithTimeout(context.Background(), hbWriteTimeout)
				pVKID, _, unknown, err := ServiceHeartbeat(ctx, myID, p.Addr, servicesToHB)
				if err != nil {
					if hbErrs != nil && !done.Load() {
						hbErrs <- err
					}
				}
				if pVKID != p.ID {
					if hbErrs != nil && !done.Load() {
						hbErrs <- fmt.Errorf("parent ID mismatch. Expected %d but ServiceHeartbeat was answered by %d. Operation complete", p.ID, pVKID)
					}
					c()
					return
				}
				// send each unknown service as an error and remove it from the services to be pinged
				for _, unk := range unknown {
					if hbErrs != nil && !done.Load() {
						hbErrs <- fmt.Errorf("service %s is considered unknown by parent @ %v", unk, p.Addr)
					}
					services = slices.DeleteFunc(services, func(svc string) bool {
						return svc == unk
					})
				}
				c()
			}
		}
	}()
	return func() {
		// only shutdown if we have not shutdown already
		if done.CompareAndSwap(false, true) {
			close(cnl)
		}
	}, nil
}

// #region client requests

// Status sends a STATUS packet to the given address and returns its answer (or an error).
// ID is optional; if given, the STATUS packet will be sent long-form.
// If omitted, the STATUS packet will be sent shorthand.
//
// This subroutine can be invoked by any node.
func Status(target netip.AddrPort, ctx context.Context, senderID ...slims.NodeID) (vkID slims.NodeID, _ *pb.StatusResp, _ error) {
	var sr *pb.StatusResp
	if !target.IsValid() {
		return 0, sr, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, sr, ErrInvalidAddrPort
	}

	// generate a header
	reqHdr := protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Shorthand: true, Type: pb.MessageType_STATUS}
	if len(senderID) > 0 {
		reqHdr.Shorthand = false
		reqHdr.ID = senderID[0]
	}

	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, sr, err
	}

	if _, err := protocol.WritePacket(ctx, conn, reqHdr, nil); err != nil {
		return 0, sr, err
	}

	// await a response
	_, _, respHdr, bd, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, sr, err
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(bd, f); err != nil {
			return respHdr.ID, sr, err
		}
		return respHdr.ID, nil, slims.FormatFault(f)
	case pb.MessageType_STATUS_RESP:
		sr := &pb.StatusResp{}
		if err := proto.Unmarshal(bd, sr); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, sr, nil
	default:
		return respHdr.ID, sr, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// List sends a LIST packet to the given address and returns the available services.
// ID is optional; if given, the LIST packet will be sent long-form.
// If token is empty, a random one will be generated.
//
// This subroutine can be invoked by any node.
func List(target netip.AddrPort, ctx context.Context, token string, hopCount uint16, senderID ...slims.NodeID) (responderAddr net.Addr, services []string, err error) {
	if !target.IsValid() {
		return nil, nil, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return nil, nil, ErrInvalidAddrPort
	}

	// if token is empty, replace it
	if strings.TrimSpace(token) == "" {
		token = randomdata.SillyName()
	}

	// generate a header
	reqHdr := protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Shorthand: true, Type: pb.MessageType_LIST}
	if len(senderID) > 0 {
		reqHdr.Shorthand = false
		reqHdr.ID = senderID[0]
	}

	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return nil, nil, err
	}

	if _, err := protocol.WritePacket(ctx, conn, reqHdr, &pb.List{
		Token:        token,
		HopCount:     uint32(hopCount),
		ResponseAddr: conn.LocalAddr().String(),
	}); err != nil {
		return nil, nil, err
	}

	// wait until we get a response
	var (
		listResp pb.ListResponse
	)
	for ctx.Err() == nil {
		_, origAddr, hdr, body, err := protocol.ReceivePacket(conn, ctx)
		if err != nil {
			return nil, nil, err
		}
		// check the token
		if hdr.Type == pb.MessageType_LIST_RESP {
			responderAddr = origAddr
			// unpack body
			if err := proto.Unmarshal(body, &listResp); err != nil {
				return nil, nil, err
			}
			if listResp.Token == token { // the response we were waiting for!
				break
			}
		}
	}
	return responderAddr, listResp.Services, nil
}

// #endregion client requests

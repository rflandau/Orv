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
	"sync/atomic"
	"time"

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
// Stale is only important if the registering node is a leaf (not a child vk).
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

// RegisterNewLeaf is an ease-of-use function that HELLOs, JOINs, and REGISTERs under the target, returning on the first failure.
// The new child will be registered as a leaf; if you are trying to join as a VK use the method on Vaultkeeper.
//
// ctx will be shared among all requests.
func RegisterNewLeaf(ctx context.Context, myID slims.NodeID, target netip.AddrPort, services map[string]struct {
	Stale time.Duration
	Addr  netip.AddrPort
}) (servicesRegistered []string, _ error) {
	if _, _, _, err := Hello(ctx, myID, target); err != nil {
		return nil, err
	}
	if _, _, err := Join(ctx, myID, target, JoinInfo{}); err != nil {
		return nil, err
	}
	// track the services that were registered in case we get interrupted
	for svc, inf := range services {
		servicesRegistered = append(servicesRegistered, svc)
		if _, _, err := Register(ctx, myID, target, svc, inf.Addr, inf.Stale); err != nil {
			return servicesRegistered, err
		}
	}
	return servicesRegistered, nil
}

// ServiceHeartbeat sends a SERVICE_HEARTBEAT packet to the given address, which must be owned by this node's parent.
// This should be run in a loop to ensure the service does not get pruned.
// Because we are using UDP and thus packets can get lost, you should repeat this if no ACK is received (otherwise the service risks being pruned).
//
// ServicesRefreshed should be checked to ensure it matches the given list of services; missing services may or may not have been refreshed.
//
// Do NOT use this for VK heartbeats. Use vk.Heartbeat() for that (or rely on the automated heartbeating this library implements in VKs).
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

// Leave alerts the target (who is, assumedly, your parent VK) that you are departing the vault.
// Future interactions (excluding client requests) will require going through the handshake again.
//
// If a fault occurs, it is transformed into an error via slims.FormatFault().
func Leave(ctx context.Context, target netip.AddrPort, senderID slims.NodeID) error {
	if !target.IsValid() {
		return ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return ErrInvalidAddrPort
	}

	// generate header
	reqHdr := protocol.Header{
		Version: protocol.SupportedVersions().HighestSupported(),
		Type:    pb.MessageType_LEAVE,
		ID:      senderID,
	}
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return err
	}
	if _, err := protocol.WritePacket(ctx, conn, reqHdr, nil); err != nil {
		return fmt.Errorf("failed to write LEAVE packet: %w", err)
	}
	// await a response
	_, _, respHdr, bd, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return fmt.Errorf("failed to receive response packet: %w", err)
	}

	// handle response
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(bd, f); err != nil {
			return fmt.Errorf("failed to unmarshal FAULT packet: %w", err)
		}
		return slims.FormatFault(f)
	case pb.MessageType_LEAVE_ACK:
		// LEAVE_ACK has no body
		return nil
	default:
		return fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// #region client requests

// Status sends a STATUS packet to the given address and returns its answer (or an error).
// ID is optional; if given, the STATUS packet will be sent long-form.
// If omitted, the STATUS packet will be sent shorthand.
//
// response struct will not be of type FAULT; FAULTs will be returned as an error per FormatFault.
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
	reqHdr := protocol.Header{
		Version:   protocol.SupportedVersions().HighestSupported(),
		Shorthand: true,
		Type:      pb.MessageType_STATUS}
	if len(senderID) > 0 {
		reqHdr.Shorthand = false
		reqHdr.ID = senderID[0]
	}

	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, sr, err
	}

	if _, err := protocol.WritePacket(ctx, conn, reqHdr, nil); err != nil {
		return 0, sr, fmt.Errorf("failed to write STATUS packet: %w", err)
	}

	// await a response
	_, _, respHdr, bd, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, sr, fmt.Errorf("failed to receive response packet: %w", err)
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(bd, f); err != nil {
			return respHdr.ID, nil, fmt.Errorf("failed to unmarshal FAULT packet: %w", err)
		}
		return respHdr.ID, nil, slims.FormatFault(f)
	case pb.MessageType_STATUS_RESP:
		sr := &pb.StatusResp{}
		if err := proto.Unmarshal(bd, sr); err != nil {
			return respHdr.ID, nil, fmt.Errorf("failed to unmarshal STATUS_RESP packet: %w\n%v", err, bd)
		}
		return respHdr.ID, sr, nil
	default:
		return respHdr.ID, sr, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// List sends a LIST packet to the given address and returns the available services.
// ID is optional; if given, the LIST packet will be sent long-form.
//
// laddr is used to receive the response.
// If the IP field of laddr is nil or an unspecified IP address,
// ListenUDP listens on all available IP addresses of the local system except multicast IP addresses.
// If the Port field of laddr is 0, a port number is automatically chosen.
//
// Assuming the request was successfully sent, List only returns if the context is cancelled/expires or a LIST_RESP is received.
// LIST_ACKs are thrown away.
//
// This subroutine can be invoked by any node.
func List(target netip.AddrPort, ctx context.Context, token string, hopCount uint16, laddr *net.UDPAddr, senderID ...slims.NodeID) (responderAddr net.Addr, services []string, err error) {
	if token == "" {
		return nil, nil, errors.New("token must not be empty")
	}
	if !target.IsValid() {
		return nil, nil, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return nil, nil, ErrInvalidAddrPort
	}

	// spool up a listener and channels to await a response.
	// both channels are closed when receive returns
	msgCh := make(chan struct {
		addr     net.Addr
		services []string
	})
	errCh := make(chan error)
	ear, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, nil, err
	}
	defer ear.Close()
	go func() {
		defer close(msgCh)
		defer close(errCh)
		// continues to receive until one of the following occurs:
		// 1. an error occurs (such as the context expiring)
		// 2. a fault is received
		// 3. a list response with the expected token is received
		for {
			_, addr, hdr, body, err := protocol.ReceivePacket(ear, ctx)
			if err != nil {
				errCh <- err
				return
			} else if hdr.Type == pb.MessageType_FAULT {
				// unpack the body
				var f pb.Fault
				if err := proto.Unmarshal(body, &f); err != nil {
					errCh <- err
					return
				}
				errCh <- slims.FormatFault(&f)
				return
			} else if hdr.Type == pb.MessageType_LIST_RESP {
				var lr pb.ListResp
				if err := proto.Unmarshal(body, &lr); err != nil {
					errCh <- err
					return
				} else if lr.Token == token {
					msgCh <- struct {
						addr     net.Addr
						services []string
					}{addr, lr.Services}
					return
				}
			}
		}
	}()

	// generate a header
	reqHdr := protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Shorthand: true, Type: pb.MessageType_LIST}
	if len(senderID) > 0 {
		reqHdr.Shorthand = false
		reqHdr.ID = senderID[0]
	}

	// send the request
	requestorConn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return nil, nil, err
	}
	if _, err := protocol.WritePacket(ctx, requestorConn, reqHdr, &pb.List{
		Token:        token,
		HopCount:     uint32(hopCount),
		ResponseAddr: ear.LocalAddr().String(),
	}); err != nil {
		return nil, nil, err
	}

	// wait until we get a response.
	// the context given to ear should cancel it and return an error over the channel for us.
	// no need to also check context.
	select {
	case msg := <-msgCh:
		return msg.addr, msg.services, nil
	case err := <-errCh:
		return nil, nil, err
	}
}

// TODO annotate
func Get(ctx context.Context, service string, target netip.AddrPort, token string, hopLimit uint16, laddr *net.UDPAddr, senderID ...slims.NodeID) (responderAddr net.Addr, serviceAddr string, err error) {
	if token == "" {
		return nil, "", errors.New("token must not be empty")
	} else if !target.IsValid() {
		return nil, "", ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return nil, "", ErrInvalidAddrPort
	}
	// spool up a listener and two channels to await a response.
	// both channels are closed when receive returns
	msgCh := make(chan struct {
		serviceAddr   string
		responderAddr net.Addr
	})
	errCh := make(chan error)
	ear, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, "", err
	}
	defer ear.Close()
	go func() {
		defer close(msgCh)
		defer close(errCh)
		// continues to receive until one of the following occurs:
		// 1. an error occurs (such as the context expiring)
		// 2. a fault is received
		// 3. a get response with the expected token is received
		for {
			_, raddr, hdr, body, err := protocol.ReceivePacket(ear, ctx)
			if err != nil {
				errCh <- err
				return
			} else if hdr.Type == pb.MessageType_FAULT {
				// unpack the body
				var f pb.Fault
				if err := proto.Unmarshal(body, &f); err != nil {
					errCh <- err
					return
				}
				errCh <- slims.FormatFault(&f)
				return
			} else if hdr.Type == pb.MessageType_GET_RESP {
				var gr pb.GetResp
				if err := proto.Unmarshal(body, &gr); err != nil {
					errCh <- err
					return
				} else if gr.Token == token {
					msgCh <- struct {
						serviceAddr   string
						responderAddr net.Addr
					}{gr.Address, raddr}
					return
				}
			}
		}
	}()

	// generate a header
	reqHdr := protocol.Header{
		Version:   protocol.SupportedVersions().HighestSupported(),
		Shorthand: true,
		Type:      pb.MessageType_GET}
	if len(senderID) > 0 {
		reqHdr.Shorthand = false
		reqHdr.ID = senderID[0]
	}

	// send the request
	requestorConn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return nil, "", err
	}
	if _, err := protocol.WritePacket(ctx, requestorConn, reqHdr, &pb.Get{
		Token:        token,
		HopLimit:     uint32(hopLimit),
		ResponseAddr: ear.LocalAddr().String(),
		Service:      service,
	}); err != nil {
		return nil, "", err
	}

	// wait until we get a response.
	// the context given to ear should cancel it and return an error over the channel for us.
	// no need to also check context.
	select {
	case msg := <-msgCh:
		return msg.responderAddr, msg.serviceAddr, nil
	case err := <-errCh:
		return nil, "", err
	}
}

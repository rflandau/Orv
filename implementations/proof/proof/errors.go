package proof

/*
Static errors for ease of use and consistency.

HErrs are errors wrapped in the tidings of Huma such that they include the packet type in the header and a status code for Huma to respond with (typically 400).
*/

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/danielgtaylor/huma/v2"
)

//#region Errors

// this VK has been terminated.
func ErrDead() error {
	return errors.New("this VaultKeeper is dead")
}

// invalid stale time
func ErrBadStaleTime() error {
	return errors.New("stale time must be a valid Go time greater than 0")
}

// invalid addrport
func ErrBadAddr(ap netip.AddrPort) error {
	return fmt.Errorf("address %v is not a valid ip:port", ap)
}

// given cID does not correspond to a known child
func ErrUnknownCID(cID childID) error {
	return fmt.Errorf("id %d does not correspond to any known child", cID)
}

// given service name is empty
func ErrEmptyServiceName(sn serviceName) error {
	return errors.New("service name cannot be empty (given " + sn + ")")
}

//#endregion Errors

//#region Huma Errors (with Hdrs)

const hdrPkt_t string = "Pkt-Type"

// id is not 0 < id <= max(uint64)
func HErrBadID(id uint64, pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest(fmt.Sprintf("id (%d) must be 0 < x <= max(uint64) and not equal to the VK's ID", id)),
		http.Header{
			hdrPkt_t: {pkt_t},
		})
}

// service name may not be empty
func HErrBadServiceName(sn string, pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest(ErrEmptyServiceName(sn).Error()),
		http.Header{
			hdrPkt_t: {pkt_t},
		})
}

func HErrBadHeight(CurVKHeight, RequesterHeight uint16, pkt_t PacketType) error {
	// TODO if a parent is available, tell the requester to try the parent
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest(fmt.Sprintf("to join as a child VK to this VK, height (%d) must be parent VK height (%d)-1", CurVKHeight, RequesterHeight)),
		http.Header{
			hdrPkt_t: {pkt_t},
		})
}

// The requester was not found in the pendingHello table and therefore did not first greet with a HELLO (or their HELLO was pruned).
func HErrMustHello(pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest("must send HELLO first"), http.Header{
			hdrPkt_t: {pkt_t},
		})
}

// The requester was not found in the children table and therefore did not first JOIN (or their JOIN was pruned).
func HErrMustJoin(pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest("must first JOIN the vault"), http.Header{
			hdrPkt_t: {pkt_t},
		})
}

// Failed to parse a valid netip.AddrPort from the given string.
func HErrBadAddr(addr_s string, pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest("failed to parse "+addr_s+" in the form <ip>:<port>"), http.Header{
			hdrPkt_t: {pkt_t},
		})
}

// Failed to parse a valid Go duration from the given string.
func HErrBadStaleness(stale_s string, pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error400BadRequest("failed to parse "+stale_s+" as a duration. Must follow Go's rules for time parsing."), http.Header{
			hdrPkt_t: {pkt_t},
		})
}

// The given child id is already in use by a different child.
func HErrIDInUse(id childID, pkt_t PacketType) error {
	return huma.ErrorWithHeaders(
		huma.Error409Conflict(fmt.Sprintf("id %d is already in use by a child of a different type", id)), http.Header{
			hdrPkt_t: {pkt_t},
		})
}

//#endregion Huma Errors (with Hdrs)

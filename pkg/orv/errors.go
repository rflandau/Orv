package orv

import (
	"fmt"
	"net/http"
	"net/netip"

	"github.com/danielgtaylor/huma/v2"
)

const hdrPkt_t string = "Pkt-Type"

// id is not 0 < id <= max(uint64)
func HErrBadID(id uint64, pkt_t PacketType) error {
	return huma.ErrorWithHeaders(fmt.Errorf("id (%d) must be 0 < x <= max(uint64)", id), http.Header{
		hdrPkt_t: {pkt_t},
	})
}

func ErrBadAddr(ap netip.AddrPort) error {
	return fmt.Errorf("address %v is not a valid ip:port", ap)
}

func HErrBadHeight(CurVKHeight, RequesterHeight uint16, pkt_t PacketType) error {
	// TODO if a parent is available, tell the requester to try the parent
	return huma.ErrorWithHeaders(fmt.Errorf("to join this VK, height (%d) must be VK height (%d)-1", CurVKHeight, RequesterHeight), http.Header{
		hdrPkt_t: {pkt_t},
	})
}

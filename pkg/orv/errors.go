package orv

import (
	"fmt"
	"net/netip"
)

const (
	ErrBadID string = "id must be 0 < x <= max(uint64)"
)

type ErrBadAddr struct {
	BadAddr netip.AddrPort
}

func (e ErrBadAddr) Error() string {
	return fmt.Sprintf("Address %v is not a valid ip:port", e.BadAddr)
}

// TODO create constructor
type ErrBadHeight struct {
	CurVKHeight     uint16
	RequesterHeight uint16
}

func (e ErrBadHeight) Error() string {
	// TODO if a parent is available, tell the requester to try the parent
	// TODO if root and VKHeight==ReqHeight, send special MERGE error message
	return fmt.Sprintf("To join this VK, height must be =%d-1 (given %d)", e.CurVKHeight, e.RequesterHeight)
}

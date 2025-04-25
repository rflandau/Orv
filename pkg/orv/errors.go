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

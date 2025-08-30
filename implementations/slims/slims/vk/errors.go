package vaultkeeper

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
)

// ErrBadAddr returns an error to indicate that the given netip.AddrPort was invalid
func ErrBadAddr(ap netip.AddrPort) error {
	return fmt.Errorf("address %v is not a valid ip:port", ap)
}

// ErrBodyNotAccepted returns an error to indicated that a body was included in a message type that does not accept a body
func ErrBodyNotAccepted(typ mt.MessageType) error {
	return errors.New("packet type " + typ.String() + " should not contain a body")
}

// ErrShorthandNotAccepted returns an error to indicated that shorthand was declared by a message type that does not support it
func ErrShorthandNotAccepted(typ mt.MessageType) error {
	return errors.New(typ.String() + " may not be send shorthand")
}

// ErrInternalError declares that an internal server error occurred (like a HTTP/S 500)
func ErrInternalError(details string) error {
	return errors.New("an internal error occurred: " + details)
}

var (
	ErrDead = errors.New("this VaultKeeper is dead")
)

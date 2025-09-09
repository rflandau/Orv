package vaultkeeper

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
)

// ErrBadAddr returns an error to indicate that the given netip.AddrPort was invalid
func ErrBadAddr(ap netip.AddrPort) error {
	return fmt.Errorf("address %v is not a valid ip:port", ap)
}

// ErrBodyNotAccepted returns an error to indicated that a body was included in a message type that does not accept a body
func ErrBodyNotAccepted(typ mt.MessageType) error {
	return errors.New("packet type " + typ.String() + " should not contain a body")
}

// ErrBodyRequired returns an error to indicated that a body was not included in a message type that requires it
func ErrBodyRequired(typ mt.MessageType) error {
	return errors.New("packet type " + typ.String() + " requires a body")
}

// ErrVersionNotSupported returns an error to indicated that a non-HELLO request specified an unsupported version
func ErrVersionNotSupported(v version.Version) error {
	return fmt.Errorf("version %v is not supported", v)
}

// ErrBadHeightJoin indicates that the JOIN failed due to mismatching heights
func ErrBadHeightJoin(ourHeight, reqHeight uint16) error {
	return fmt.Errorf("to join as a child VK to this VK, height (%d) must equal parent VK height (%d)-1", reqHeight, ourHeight)
}

// ErrShorthandNotAccepted returns an error to indicated that shorthand was declared by a message type that does not support it
func ErrShorthandNotAccepted(typ mt.MessageType) error {
	return errors.New(typ.String() + " may not be send shorthand")
}

// ErrInternalError declares that an internal server error occurred (like a HTTP/S 500)
func ErrInternalError(details string) error {
	return errors.New("an internal error occurred: " + details)
}

// ErrFailedToUnmarshal indicates that the payload failed to unmarshal as the expected type.
func ErrFailedToUnmarshal(typ mt.MessageType, err error) error {
	return fmt.Errorf("failed to unmarshal body as a %s message: %w", typ.String(), err)
}

var (
	ErrDead = errors.New("this VaultKeeper is dead")
)

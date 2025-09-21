package vaultkeeper

import (
	"errors"
	"fmt"
	"net/netip"
)

// ErrBadAddr returns an error to indicate that the given netip.AddrPort was invalid
func ErrBadAddr(ap netip.AddrPort) error {
	return fmt.Errorf("address %v is not a valid ip:port", ap)
}

// ErrInternalError declares that an internal server error occurred (like a HTTP/S 500)
func ErrInternalError(details string) error {
	return errors.New("an internal error occurred: " + details)
}

var (
	ErrDead = errors.New("this VaultKeeper is dead")
)

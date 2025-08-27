package vaultkeeper

import (
	"errors"
	"fmt"
	"net/netip"
)

// invalid addrport
func ErrBadAddr(ap netip.AddrPort) error {
	return fmt.Errorf("address %v is not a valid ip:port", ap)
}

var ErrDead = errors.New("this VaultKeeper is dead")

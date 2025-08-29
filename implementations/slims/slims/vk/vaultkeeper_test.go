package vaultkeeper

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/slims/client"
)

func TestVaultKeeper_StartStop(t *testing.T) {
	vk, err := New(1, netip.MustParseAddrPort("127.0.0.1:8081"))
	if err != nil {
		t.Fatal(err)
	}

	startAndCheck(t, vk)
	stopAndCheck(t, vk)

	startAndCheck(t, vk)
	stopAndCheck(t, vk)

	startAndCheck(t, vk)
	stopAndCheck(t, vk)

	startAndCheck(t, vk)
	stopAndCheck(t, vk)
}

// helper function.
// Calls .Start() on the given VK, then ensures we can successfully hit it with a STATUS packet.
func startAndCheck(t *testing.T, vk *VaultKeeper) {
	if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	if alive, err := upstate(t, vk); !alive {
		t.Fatal("vk is dead after starting")
	} else if err != nil {
		t.Fatal(err)
	}
}

// helper function.
// Calls .Stop() on the given VK, then ensures that STATUS fails against it.
func stopAndCheck(t *testing.T, vk *VaultKeeper) {
	vk.Stop()
	if alive, err := upstate(t, vk); alive {
		t.Fatal("vk is alive after stopping")
	} else if err == nil {
		t.Fatal("expected an error trying to hit STATUS on a stopped vk")
	}
}

// upstate returns the alive state of the given vk and the result of trying to hit it with a STATUS packet (no matter its declared `alive` status).
func upstate(t *testing.T, vk *VaultKeeper) (alive bool, srErr error) {
	t.Helper()
	alive = vk.net.accepting.Load()

	// send a STATUS packet
	if sr, err := client.Status(netip.MustParseAddrPort(vk.addr.String()), t.Context()); err != nil {
		return alive, err
	} else if sr == nil {
		return alive, errors.New("nil response")
	}

	return alive, nil
}

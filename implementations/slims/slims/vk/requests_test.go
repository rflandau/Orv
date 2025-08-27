package vaultkeeper_test

import (
	"net/netip"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
	. "github.com/rflandau/Orv/internal/testsupport"
)

func TestVaultKeeper_Hello(t *testing.T) {
	// TODO spin up two VKs and call Hello() on one
	// check the response for teh sender and the Hello table of the receiver

	targetVKAddr := netip.MustParseAddrPort("[::0]:8081")
	targetVK, err := vaultkeeper.New(1, targetVKAddr)
	if err != nil {
		t.Fatal(err)
	}
	targetVK.Start()
	defer targetVK.Stop()
	senderVK, err := vaultkeeper.New(2, netip.MustParseAddrPort("[::0]:8082"))
	if err != nil {
		t.Fatal(err)
	}
	senderVK.Start()
	defer senderVK.Stop()

	resp, err := senderVK.Hello(targetVKAddr.String(), t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Height != 0 {
		t.Error(ExpectedActual(0, resp.Height))

	} else if resp.Id != 2 {
		t.Error(ExpectedActual(2, resp.Id))
	} else if byte(resp.Version) != protocol.HighestSupported.Byte() {
		t.Error(ExpectedActual(protocol.HighestSupported.Byte(), resp.Version))
	}
}

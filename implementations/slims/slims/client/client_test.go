package client

import (
	"context"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
	. "github.com/rflandau/Orv/internal/testsupport"
)

func TestStatus(t *testing.T) {
	pingTimeout := 300 * time.Millisecond

	// Spawn a VK to hit
	vk, err := vaultkeeper.New(1, netip.MustParseAddrPort("[::0]:8081"))
	if err != nil {
		t.Fatal(err)
	}

	vk.Start()

	// ensure we can ping the vk
	if err := CoAPPing("[::0]:8081", pingTimeout); err != nil {
		t.Fatal(err)
	}

	// submit a status request
	ctx, cancel := context.WithTimeout(t.Context(), pingTimeout)
	defer cancel()
	resp, err := Status("[::0]:8081", ctx)
	if err != nil {
		t.Fatal(err)
	}
	// validate the fields in resp
	if resp.Height != 0 {
		t.Error("bad height", ExpectedActual(0, resp.Height))
	}
	if resp.Id != 1 {
		t.Error("bad vkID", ExpectedActual(1, resp.Id))
	}
	actualVersions := protocol.VersionsSupportedAsBytes()
	if slices.Compare(actualVersions, resp.VersionsSupported) != 0 {
		t.Error("mismatching version list", ExpectedActual(resp.VersionsSupported, actualVersions))
	}
}

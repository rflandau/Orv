// Package slims_test implements implementation-wide tests.
package slims_test

import (
	"context"
	"math"
	"math/rand/v2"
	"net/netip"
	"strconv"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/slims/vk"
	. "github.com/rflandau/Orv/internal/testsupport"
)

func TestHello(t *testing.T) {
	var (
		vkid, nodeID = rand.Uint64(), rand.Uint64()
		port         = uint16(rand.UintN(math.MaxUint16))
		ap           = netip.MustParseAddrPort("127.0.0.1:" + strconv.FormatUint(uint64(port), 10))
	)
	// spawn a VK
	vk, err := vaultkeeper.New(vkid, ap, vaultkeeper.WithDragonsHoard(2))
	if err != nil {
		t.Fatal(err)
	} else if err := vk.Start(); err != nil {
		t.Fatal(err)
	}

	// send a Hello
	respVKID, respVersion, respBody, err := client.Hello(context.Background(), nodeID, ap)
	if err != nil {
		t.Fatal(err)
	} else if respVKID != vkid {
		t.Fatal(ExpectedActual(vkid, respVKID))
	} else if respBody.Height != 2 {
		t.Fatal(ExpectedActual(2, respBody.Height))
	} else if respVersion != protocol.SupportedVersions().HighestSupported() {
		t.Fatal(ExpectedActual(protocol.SupportedVersions().HighestSupported(), respVersion))
	}
}

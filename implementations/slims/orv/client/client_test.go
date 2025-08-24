package orv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/rflandau/Orv/implementations/proof/orv"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol"
	vaultkeeper "github.com/rflandau/Orv/implementations/slims/orv/vk"
	. "github.com/rflandau/Orv/internal/testsupport"
)

func TestStatus(t *testing.T) {
	// Spawn a VK to hit
	vk, err := vaultkeeper.New(1, netip.MustParseAddrPort("[::0]:8081"))
	if err != nil {
		t.Fatal(err)
	}

	vk.Start()

	// ensure we can ping the vk
	if err := CoAPPing("[::0]:8081", 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// submit a status request
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	resp, rawJSON, err := Status("[::0]:8081", ctx)
	if err != nil {
		t.Fatal(err)
	}
	// validate the fields in resp
	if resp.Height != 0 {
		t.Error("bad height", ExpectedActual(0, resp.Height))
	}
	if resp.ID != 1 {
		t.Error("bad vkID", ExpectedActual(1, resp.ID))
	}
	actualVersions := protocol.VersionsSupportedAsBytes()
	if slices.Compare(actualVersions, resp.VersionsSupported) != 0 {
		t.Error("mismatching version list", ExpectedActual(resp.VersionsSupported, actualVersions))
	}
	// unmarshal the JSON ourself to ensure it matches resp
	dc := json.NewDecoder(bytes.NewReader(rawJSON))
	var unmarshaled orv.StatusResp
	if err := dc.Decode(&unmarshaled); err != nil {
		t.Error(err)
	}
}

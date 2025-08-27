package vaultkeeper

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	. "github.com/rflandau/Orv/internal/testsupport"
)

func TestVaultKeeper_StartStop(t *testing.T) {
	vk, err := New(1, netip.MustParseAddrPort("127.0.0.1:8081"))
	if err != nil {
		t.Fatal(err)
	}

	const timeout time.Duration = 300 * time.Millisecond

	vk.Start()
	if err := CoAPPing("127.0.0.1:8081", timeout); err != nil {
		t.Error(err)
	}
	vk.Stop()
	// testsupport.CoAPPing the server again, but expect failure
	if err := CoAPPing("127.0.0.1:8081", timeout); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("bad testsupport.CoAPPing result. Expected DeadlineExceeded error, found ", err)
	}

	vk.Start()
	if err := CoAPPing("127.0.0.1:8081", timeout); err != nil {
		t.Error(err)
	}
	vk.Stop()
	if err := CoAPPing("127.0.0.1:8081", timeout); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("bad testsupport.CoAPPing result. Expected DeadlineExceeded error, found ", err)
	}

	vk.Start()
	if err := CoAPPing("127.0.0.1:8081", timeout); err != nil {
		t.Error(err)
	}
	vk.Stop()
	if err := CoAPPing("127.0.0.1:8081", timeout); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("bad testsupport.CoAPPing result. Expected DeadlineExceeded error, found ", err)
	}
}

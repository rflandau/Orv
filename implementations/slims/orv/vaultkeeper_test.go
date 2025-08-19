package orv

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/udp"
)

func TestVaultKeeper_StartStop(t *testing.T) {
	vk, err := NewVaultKeeper(1, netip.MustParseAddrPort("127.0.0.1:8081"))
	if err != nil {
		t.Fatal(err)
	}

	const timeout time.Duration = 300 * time.Millisecond

	vk.Start()
	if err := ping("127.0.0.1:8081", timeout); err != nil {
		t.Error(err)
	}
	vk.Stop()
	// ping the server again, but expect failure
	if err := ping("127.0.0.1:8081", timeout); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("bad ping result. Expected DeadlineExceeded error, found ", err)
	}

	vk.Start()
	if err := ping("127.0.0.1:8081", timeout); err != nil {
		t.Error(err)
	}
	vk.Stop()
	if err := ping("127.0.0.1:8081", timeout); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("bad ping result. Expected DeadlineExceeded error, found ", err)
	}

	vk.Start()
	if err := ping("127.0.0.1:8081", timeout); err != nil {
		t.Error(err)
	}
	vk.Stop()
	if err := ping("127.0.0.1:8081", timeout); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Error("bad ping result. Expected DeadlineExceeded error, found ", err)
	}
}

func ping(addr string, timeout time.Duration) error {
	// ping the server
	conn, err := udp.Dial(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		return err
	}
	return nil
}

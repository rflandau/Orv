package vaultkeeper_test

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/tcp/server"
	"github.com/rs/zerolog"
)

func TestVaultKeeper_Hello(t *testing.T) {
	// TODO spin up two VKs and call Hello() on one
	// check the response for teh sender and the Hello table of the receiver

	type fields struct {
		alive atomic.Bool
		log   *zerolog.Logger
		id    uint64
		addr  netip.AddrPort
		net   struct {
			alive    atomic.Bool
			listener *net.UDPConn
			server   *server.Server
		}
		mux       *mux.Router
		structure struct {
			mu         sync.RWMutex
			height     uint16
			parentID   uint64
			parentAddr netip.AddrPort
		}
	}
	type args struct {
		addrPort string
		ctx      context.Context
	}
	tests := []struct {
		name         string
		fields       fields
		args         args
		wantResponse HelloAck
		wantErr      bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vk := &VaultKeeper{
				alive:     tt.fields.alive,
				log:       tt.fields.log,
				id:        tt.fields.id,
				addr:      tt.fields.addr,
				net:       tt.fields.net,
				mux:       tt.fields.mux,
				structure: tt.fields.structure,
			}
			gotResponse, err := vk.Hello(tt.args.addrPort, tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("VaultKeeper.Hello() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotResponse, tt.wantResponse) {
				t.Errorf("VaultKeeper.Hello() = %v, want %v", gotResponse, tt.wantResponse)
			}
		})
	}
}

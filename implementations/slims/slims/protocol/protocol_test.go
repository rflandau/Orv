package protocol_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"google.golang.org/protobuf/proto"
)

// Tests that Serialize puts out the expected byte string fromm a given header struct.
// Serialize does not validate data in header, so errors can only come from a failure to write and thus are always fatal.
func TestHeader_SerializeWithValidate(t *testing.T) {
	tests := []struct {
		name string
		hdr  protocol.Header
		want struct {
			version       byte         // outcome byte
			shorthandType byte         // outcome composite byte (0x10000000 & type)
			id            slims.NodeID // last 8 bytes converted back into a uint64 (if shorthand was unset)
		}
		invalids []error
	}{
		// long form
		{name: "all zeros",
			hdr: protocol.Header{},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "1.0",
			hdr: protocol.Header{Version: version.Version{Major: 1, Minor: 0}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b00010000, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "3.13",
			hdr: protocol.Header{Version: version.Version{Major: 3, Minor: 13}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x3D, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "JOIN",
			hdr: protocol.Header{Type: pb.MessageType_JOIN},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0x4, 0},
			invalids: []error{},
		},
		{name: "15.15 MERGE_ACCEPT",
			hdr: protocol.Header{Version: version.Version{Major: 15, Minor: 15}, Type: pb.MessageType_MERGE_ACCEPT},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0xFF, byte(pb.MessageType_MERGE_ACCEPT), 0},
			invalids: []error{},
		},
		{name: "0.7 Get ID=15",
			hdr: protocol.Header{Version: version.Version{Major: 0, Minor: 7}, Type: pb.MessageType_GET, ID: 15},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x7, byte(pb.MessageType_GET), 15},
			invalids: []error{},
		},
		{name: "0.7 Get ID=MaxUint64",
			hdr: protocol.Header{Version: version.Version{Major: 0, Minor: 7}, Type: pb.MessageType_GET, ID: math.MaxUint64},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x7, byte(pb.MessageType_GET), math.MaxUint64},
			invalids: []error{},
		},

		// shorthand
		{name: "shorthand",
			hdr: protocol.Header{Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "3.13 shorthand",
			hdr: protocol.Header{Version: version.Version{Major: 3, Minor: 13}, Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x3D, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},

		// invalids
		{name: "invalid major version",
			hdr: protocol.Header{Version: version.Version{Major: 31, Minor: 0}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b11110000, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMajor},
		},
		{name: "invalid major version with type",
			hdr: protocol.Header{Version: version.Version{Major: 32, Minor: 0}, Type: pb.MessageType_FAULT},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b0000, 0b00000001, 0},
			invalids: []error{protocol.ErrInvalidVersionMajor},
		},
		{name: "invalid minor version",
			hdr: protocol.Header{Version: version.Version{Major: 0, Minor: 255}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b00001111, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMinor},
		},
		{name: "invalid minor version shorthand",
			hdr: protocol.Header{Version: version.Version{Major: 0, Minor: 255}, Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b00001111, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMinor},
		},
		{name: "shorthand ID",
			hdr: protocol.Header{Shorthand: true, ID: 55},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0b10000000, 55},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrShorthandID},
		},
		{name: "shorthand ID with type",
			hdr: protocol.Header{Shorthand: true, Type: pb.MessageType_INCREMENT, ID: 1},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0b10000000 | byte(pb.MessageType_INCREMENT), 1},
			invalids: []error{protocol.ErrShorthandID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if vErrors := tt.hdr.Validate(); !SlicesUnorderedEqual(vErrors, tt.invalids) {
				t.Error("incorrect validation errors",
					ExpectedActual(tt.invalids, vErrors),
				)
			}

			got, err := tt.hdr.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			// length check
			if tt.hdr.Shorthand && (len(got) != int(protocol.ShortHeaderLen)) {
				t.Fatal("incorrect bytestream length", ExpectedActual(int(protocol.ShortHeaderLen), len(got)))
			} else if !tt.hdr.Shorthand && (len(got) != int(protocol.LongHeaderLen)) {
				t.Fatal("incorrect bytestream length", ExpectedActual(int(protocol.LongHeaderLen), len(got)))
			}
			// validate individual bytes
			if got[0] != tt.want.version {
				t.Error("bad version byte", ExpectedActual(tt.want.version, got[0]))
			} else if got[1] != tt.want.shorthandType {
				t.Error("bad shorthand/type composite byte", ExpectedActual(tt.want.shorthandType, got[1]))
			}

			if !tt.hdr.Shorthand { // check the ID
				if gotID := binary.BigEndian.Uint64(got[2:]); gotID != tt.want.id {
					t.Error("bad ID", ExpectedActual(tt.want.id, gotID))
				}
			}

		})
	}
}

// Tests the edge cases of Deserialize. Most Deserialize testing is done in FullSend.
func TestDeserialize(t *testing.T) {
	t.Run("empty buffer short read", func(t *testing.T) {
		hdr, err := protocol.Deserialize(bytes.NewReader([]byte{}))
		if err == nil {
			t.Error("expected a short read error, found none")
		}
		// ensure that hdr is empty
		var zero protocol.Header
		if hdr != zero {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(zero, hdr))
		}
	})
	t.Run("single byte short read", func(t *testing.T) {
		var byt byte = 0b00100111 // 2.7
		hdr, err := protocol.Deserialize(bytes.NewReader([]byte{byt}))
		if err == nil {
			t.Error("expected a short read error, found none")
		}
		// ensure that hdr is empty
		want := protocol.Header{Version: version.FromByte(byt)}
		if hdr != want {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(want, hdr))
		}
	})
	t.Run("shorthand", func(t *testing.T) {
		var byt byte = 0b00100111 // 2.7
		hdr, err := protocol.Deserialize(bytes.NewReader([]byte{byt, 0b10000010}))
		if err != nil {
			t.Error(err)
		}
		// ensure that hdr is empty
		want := protocol.Header{
			Version:   version.FromByte(byt),
			Shorthand: true,
			Type:      pb.MessageType_HELLO,
		}
		if hdr != want {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(want, hdr))
		}
	})
	t.Run("missing ID", func(t *testing.T) {
		var byt byte = 0b01100110 // 6.6
		hdr, err := protocol.Deserialize(bytes.NewReader([]byte{byt, 0b00000011}))
		if err == nil {
			t.Error("expected a short read error, found none")
		}
		// ensure that hdr is empty
		want := protocol.Header{Version: version.FromByte(byt), Type: pb.MessageType_HELLO_ACK}
		if hdr != want {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(want, hdr))
		}
	})

}

// Spins up a server to deserialize, ~~validate~~, serialize, and echo back whatever is sent to it.
func TestFullSend(t *testing.T) {
	curVer, err := version.New(8, 1)
	if err != nil {
		t.Fatal(err)
	}

	var (
		echoServerID slims.NodeID = 1
		done         atomic.Bool
		port         = RandomPort()
	)
	// generate a server to echo data back
	pconn, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:"+strconv.FormatUint(uint64(port), 10))
	if err != nil {
		t.Fatal(err)
	}
	t.Log("listening @ ", pconn.LocalAddr())
	t.Cleanup(func() {
		pconn.Close()
		done.Store(true)
	})

	var pktbuf = make([]byte, slims.MaxPacketSize)
	go func() {
		for {
			var (
				rcvdN      int
				reqHdr     protocol.Header
				senderAddr net.Addr
			)
			{ // receive
				rcvdN, senderAddr, reqHdr, _, err = protocol.ReceivePacket(pconn, t.Context())
				if rcvdN == 0 {
					if done.Load() {
						return
					}
					continue
				} else if err != nil {
					// respond with a FAULT
					pkt, serializeErr := protocol.Serialize(
						curVer, false, pb.MessageType_FAULT, echoServerID,
						&pb.Fault{Original: pb.MessageType_UNKNOWN, Errno: pb.Fault_UNSPECIFIED},
					)
					if serializeErr != nil {
						t.Error(serializeErr)
					}
					if _, err := pconn.WriteTo(pkt, senderAddr); err != nil {
						t.Error(err)
					}
					continue
				}
				t.Logf("read %d bytes", rcvdN)
			}

			// validate
			if errs := reqHdr.Validate(); errs != nil {
				// respond with a FAULT
				var errStr strings.Builder
				for _, err := range errs {
					errStr.WriteString(err.Error() + "\n")
				}
				ai := errStr.String()[:errStr.Len()-1]
				pkt, serializeErr := protocol.Serialize(
					reqHdr.Version, false, pb.MessageType_FAULT, echoServerID,
					&pb.Fault{Original: reqHdr.Type, Errno: pb.Fault_UNSPECIFIED, AdditionalInfo: &ai},
				)
				if serializeErr != nil {
					t.Error(serializeErr)
				}

				if _, err := pconn.WriteTo(pkt, senderAddr); err != nil {
					t.Error(err)
				}
				continue
			}

			// re-serialize, but update ID
			reqHdr.ID = echoServerID
			respHdrB, err := reqHdr.Serialize()
			if err != nil {
				ai := err.Error()
				// respond with a FAULT
				b, serializeErr := protocol.Serialize(
					reqHdr.Version, false, pb.MessageType_FAULT, echoServerID,
					&pb.Fault{Original: reqHdr.Type, Errno: pb.Fault_UNSPECIFIED, AdditionalInfo: &ai},
				)
				if serializeErr != nil {
					t.Error(serializeErr)
				}
				if _, err := pconn.WriteTo(append(b, []byte(err.Error())...), senderAddr); err != nil {
					t.Error(err)
				}
				continue
			}

			if sentN, err := pconn.WriteTo(respHdrB, senderAddr); err != nil { // TODO echo back body
				t.Error(err)
			} else if rcvdN != sentN {
				t.Errorf("sent (%d) != received (%d)", sentN, rcvdN)
			}
			// clear the buffer
			clear(pktbuf)
		}
	}()

	tests := []struct {
		name         string
		header       *protocol.Header
		body         []byte
		wantRespType pb.MessageType
	}{
		{"1.1, long, HELLO, ID0", &protocol.Header{Version: version.Version{Major: 1, Minor: 1}, Type: pb.MessageType_HELLO}, nil, pb.MessageType_HELLO},
		{"1.1, long, [unknown message type], ID0", &protocol.Header{Version: version.Version{Major: 1, Minor: 1}, Type: 50}, nil, 50},
		{"0.0, long, MERGE, ID22", &protocol.Header{Type: pb.MessageType_MERGE, ID: 22}, nil, pb.MessageType_MERGE},
		{"15.2, short, MERGE, ID22", &protocol.Header{Version: version.Version{Major: 15, Minor: 2}, Shorthand: true, Type: pb.MessageType_MERGE}, nil, pb.MessageType_MERGE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// serialize the header
			hdrB, err := tt.header.Serialize()
			if err != nil {
				t.Fatal(err)
			}

			// spawn a client to ping the server
			cli, err := net.DialUDP("udp", nil, &net.UDPAddr{Port: int(port)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cli.Close() })
			// send the serialized header
			if n, err := cli.Write(hdrB); err != nil {
				t.Error(err)
			} else if n != len(hdrB) {
				t.Error("incorrect written count", ExpectedActual(len(hdrB), n))
			}
			// await a response
			respBuf := make([]byte, slims.MaxPacketSize)
			if n, err := cli.Read(respBuf); err != nil {
				t.Error(err)
			} else {
				t.Logf("client received %d bytes", n)
			}

			respHdr, err := protocol.Deserialize(bytes.NewReader(respBuf))
			if err != nil {
				t.Error(err)
			}

			// validate response
			if tt.header.Version != respHdr.Version { // should be echo'd back exactly
				t.Error("sent version (expected) does not equal received version (actual)", ExpectedActual(tt.header.Version, respHdr.Version))
			} else if tt.header.Shorthand != respHdr.Shorthand { // should be echo'd back exactly
				t.Error("sent shorthand (expected) does not equal received shorthand (actual)", ExpectedActual(tt.header.Shorthand, respHdr.Shorthand))
			} else if (!tt.header.Shorthand && (respHdr.ID != echoServerID)) || tt.header.Shorthand && (respHdr.ID != 0) {
				t.Error("bad echo server ID", ExpectedActual(echoServerID, respHdr.ID))
			} else if respHdr.Type != tt.wantRespType {
				t.Error("bad response message type", ExpectedActual(tt.wantRespType, respHdr.Type))
			}

		})
	}
}

// Tests the edge cases of parameters.
func TestReceivePacketValidation(t *testing.T) {
	listenAddr := RandomLocalhostAddrPort().String()

	t.Run("nil connection", func(t *testing.T) {
		if n, _, _, _, err := protocol.ReceivePacket(nil, t.Context()); !errors.Is(err, protocol.ErrConnIsNil) {
			t.Fatal(ExpectedActual(protocol.ErrConnIsNil, err))
		} else if n != 0 {
			t.Fatal(ExpectedActual(0, n))
		}
	})
	t.Run("nil context", func(t *testing.T) {
		pconn, err := net.ListenPacket("udp", listenAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer pconn.Close()
		if n, _, _, _, err := protocol.ReceivePacket(pconn, nil); !errors.Is(err, slims.ErrNilCtx) {
			t.Fatal(ExpectedActual(slims.ErrNilCtx, err))
		} else if n != 0 {
			t.Fatal(ExpectedActual(0, n))
		}
	})
	t.Run("connection already closed", func(t *testing.T) {
		pconn, err := net.ListenPacket("udp", listenAddr)
		if err != nil {
			t.Fatal(err)
		}
		// close it immediately
		pconn.Close()

		if n, _, _, _, err := protocol.ReceivePacket(pconn, t.Context()); !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		} else if n != 0 {
			t.Fatal(ExpectedActual(0, n))
		}
	})
	t.Run("context already cancelled", func(t *testing.T) {
		pconn, err := net.ListenPacket("udp", listenAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer pconn.Close()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if n, _, _, _, err := protocol.ReceivePacket(pconn, ctx); err == nil {
			t.Fatal(err)
		} else if n != 0 {
			t.Fatal(ExpectedActual(0, n))
		}
	})
}

// Tests that we can send and receive packets back to back.
func TestReceivePacket(t *testing.T) {
	var (
		listenAddr = RandomLocalhostAddrPort().String()
	)
	curVer, err := version.New(1, 5)
	if err != nil {
		t.Fatal(err)
	}

	// spin up listener
	pconn, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pconn.Close() })

	// make a channel to fetch results
	ch := make(chan struct {
		n        int
		addr     net.Addr
		header   protocol.Header
		respBody []byte
		err      error
	})

	// define tests
	tests := []struct {
		hdr protocol.Header
	}{
		{protocol.Header{Version: curVer, Shorthand: false, Type: pb.MessageType_HELLO, ID: rand.Uint64()}},
		{protocol.Header{Version: version.FromByte(0b01100001), Shorthand: false, Type: pb.MessageType_HELLO, ID: rand.Uint64()}},
		{protocol.Header{Version: version.FromByte(0b01100011), Shorthand: true, Type: pb.MessageType_GET}},
	}

	// start listening for packets equal to the number of tests; forward them
	go func() {
		for range tests {
			n, addr, respHdr, respBody, err := protocol.ReceivePacket(pconn, context.Background())
			ch <- struct {
				n        int
				addr     net.Addr
				header   protocol.Header
				respBody []byte
				err      error
			}{n, addr, respHdr, respBody, err}
		}

	}()

	for i, tt := range tests {
		t.Run("basic read "+strconv.FormatInt(int64(i), 10), func(t *testing.T) {})
		// serialize
		hdrB, err := tt.hdr.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		// send
		wroteN, err := pconn.WriteTo(hdrB, pconn.LocalAddr())
		if err != nil {
			t.Fatal(err)
		}
		res := <-ch
		// compare
		if res.err != nil {
			t.Error(err)
		} else if res.n != wroteN {
			t.Error(ExpectedActual(wroteN, res.n))
		} else if res.header != tt.hdr {
			t.Error(ExpectedActual(tt.hdr, res.header))
		}
	}
	t.Run("cancel during read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// spool the receiver back up, using the context
		go func() {
			n, addr, respHdr, respBody, err := protocol.ReceivePacket(pconn, ctx)
			ch <- struct {
				n        int
				addr     net.Addr
				header   protocol.Header
				respBody []byte
				err      error
			}{n, addr, respHdr, respBody, err}
		}()
		time.Sleep(5 * time.Millisecond) // give the receiver time to start listening
		cancel()
		res := <-ch
		if !errors.Is(res.err, context.Canceled) {
			t.Fatal(ExpectedActual(context.Canceled, res.err))
		}

	})
	// make sure we can still successfully receive after the prior test
	t.Run("successful receive after prior cancel", func(t *testing.T) {
		go func() {
			n, addr, respHdr, respBody, err := protocol.ReceivePacket(pconn, context.TODO())
			ch <- struct {
				n        int
				addr     net.Addr
				header   protocol.Header
				respBody []byte
				err      error
			}{n, addr, respHdr, respBody, err}
		}()

		hdr := protocol.Header{ID: rand.Uint64()}
		hdrB, err := hdr.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		// send
		wroteN, err := pconn.WriteTo(hdrB, pconn.LocalAddr())
		if err != nil {
			t.Fatal(err)
		}
		// receive
		res := <-ch
		if res.err != nil {
			t.Error(err)
		} else if res.n != wroteN {
			t.Error(ExpectedActual(wroteN, res.n))
		} else if res.header != hdr {
			t.Error(ExpectedActual(hdr, res.header))
		}
	})
	t.Run("deadline expires during read", func(t *testing.T) {
		give := 5 * time.Millisecond // extra buffer time, as elapsed will not exactly equal timeout
		timeout := 60 * time.Millisecond
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()

		start := time.Now()
		_, _, _, _, err := protocol.ReceivePacket(pconn, ctx)
		if err == nil {
			t.Fatal("error should not be nil after failing to due to timeout")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			_, ok := err.(*net.OpError)
			if !ok {
				t.Fatal(ExpectedActual(context.DeadlineExceeded, err))
			}
		}

		elapsed := time.Since(start)
		if elapsed < timeout-time.Duration(give) {
			t.Errorf("likely timed out too quickly. %v elapsed but %v was the expected timeout", elapsed, timeout)
		}
	})
	// now send a packet within a deadline
	t.Run("successful read under deadline", func(t *testing.T) {
		timeout := 100 * time.Millisecond
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()

		go func() {
			n, addr, respHdr, respBody, err := protocol.ReceivePacket(pconn, ctx)
			ch <- struct {
				n        int
				addr     net.Addr
				header   protocol.Header
				respBody []byte
				err      error
			}{n, addr, respHdr, respBody, err}
		}()

		sentVersion := version.FromByte(0b00001000)
		sentShorthand := false
		sentType := pb.MessageType_HELLO_ACK
		sentID := rand.Uint64()
		payload := &pb.HelloAck{Height: 5}
		hdrB, err := protocol.Serialize(sentVersion, sentShorthand, sentType, sentID, payload)
		if err != nil {
			t.Fatal(err)
		}
		// send
		wroteN, err := pconn.WriteTo(hdrB, pconn.LocalAddr())
		if err != nil {
			t.Fatal(err)
		}
		// receive
		res := <-ch
		// check
		if res.err != nil {
			t.Error(err)
		} else if res.n != wroteN {
			t.Error(ExpectedActual(wroteN, res.n))
		} else if res.addr.String() != pconn.LocalAddr().String() || res.addr.Network() != pconn.LocalAddr().Network() {
			t.Error(ExpectedActual(pconn.LocalAddr(), res.addr))
		} else if res.header.ID != sentID {
			t.Error(ExpectedActual(sentID, res.header.ID))
		} else if res.header.Type != sentType {
			t.Error(ExpectedActual(sentType, res.header.Type))
		} else if res.header.Version != sentVersion {
			t.Error(ExpectedActual(sentVersion, res.header.Version))
		} else if res.header.Shorthand != sentShorthand {
			t.Error(ExpectedActual(sentShorthand, res.header.Shorthand))
		}
		// check the body
		ha := pb.HelloAck{}
		if err := proto.Unmarshal(res.respBody, &ha); err != nil {
			t.Error(err)
		}
		if ha.Height != payload.Height {
			t.Error(ExpectedActual(ha.String(), payload.String()))
		}

	})
}

// Tests Test_WritePacket to ensure we can write packets, respect contexts, and write packets independently (prior writes do not influence future writes/deadlines/cancellations).
//
// ! Does not verify the integrity of the data beyond checking the # of bytes written. That should probably be remedied eventually.
func TestWritePacket(t *testing.T) {
	// helper function to make a basic UDP connection we can write to and easily clean up
	startUDPListener := func(t *testing.T) (_ *net.UDPConn, _ func()) {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0") // allows OS to choose a port
		if err != nil {
			t.Fatalf("ResolveUDPAddr failed: %v", err)
		}
		t.Helper()

		srv, err := net.ListenUDP("udp", addr)
		if err != nil {
			t.Fatal(err)
		}

		// dial the listener
		sender, err := net.DialUDP("udp", nil, srv.LocalAddr().(*net.UDPAddr))
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}

		// reads continually until socket is closed
		go func() {
			buf := make([]byte, slims.MaxPacketSize)
			for {
				_, _, err := srv.ReadFromUDP(buf)
				if err != nil {
					// Expected once the socket is closed.
					return
				}
			}
		}()

		doneFunc := func() {
			// close both ends; the server goroutine will exit
			sender.Close()
			srv.Close()
		}
		return sender, doneFunc
	}

	t.Run("nil context", func(t *testing.T) {
		conn, done := startUDPListener(t)
		defer done()

		_, err := protocol.WritePacket(nil, conn, protocol.Header{}, nil)
		if !errors.Is(err, slims.ErrNilCtx) {
			t.Fatal(ExpectedActual(slims.ErrNilCtx, err))
		}
	})
	conn, done := startUDPListener(t)
	defer done()
	t.Run("basic", func(t *testing.T) {
		n, err := protocol.WritePacket(context.Background(), conn,
			protocol.Header{
				Version: protocol.SupportedVersions().HighestSupported(),
				Type:    pb.MessageType_STATUS,
				ID:      1,
			},
			nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != int(protocol.LongHeaderLen) {
			t.Fatal("incorrect bytes written", ExpectedActual(int(protocol.LongHeaderLen), n))
		}
	})
	t.Run("context already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancel

		n, err := protocol.WritePacket(ctx, conn, protocol.Header{}, nil)

		if n != 0 {
			t.Fatalf("expected 0 bytes written on cancellation, got %d", n)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatal(ExpectedActual(context.Canceled, err))
		}
	})
	// repeat basic a couple times on the same connection to ensure we are successfully able to write
	t.Run("basic", func(t *testing.T) {
		n, err := protocol.WritePacket(context.Background(), conn,
			protocol.Header{
				Version: protocol.SupportedVersions().HighestSupported(),
				Type:    pb.MessageType_STATUS,
				ID:      1,
			},
			nil)
		if err != nil {
			t.Fatal(err)
		}
		if n != int(protocol.LongHeaderLen) {
			t.Fatal("incorrect bytes written", ExpectedActual(int(protocol.LongHeaderLen), n))
		}
	})
	t.Run("basic", func(t *testing.T) {
		n, err := protocol.WritePacket(context.Background(), conn,
			protocol.Header{
				Version: protocol.SupportedVersions().HighestSupported(),
				Type:    pb.MessageType_STATUS,
				ID:      1,
			},
			nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != int(protocol.LongHeaderLen) {
			t.Fatal("incorrect bytes written", ExpectedActual(int(protocol.LongHeaderLen), n))
		}
	})
}

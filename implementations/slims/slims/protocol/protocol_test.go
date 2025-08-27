package protocol_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"net"
	"strings"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	. "github.com/rflandau/Orv/internal/testsupport"
)

// Tests that Serialize puts out the expected byte string fromm a given header struct.
// Serialize does not validate data in header, so errors can only come from a failure to write and thus are always fatal.
func TestHeader_SerializeWithValidate(t *testing.T) {
	tests := []struct {
		name string
		hdr  protocol.Header
		want struct {
			version       byte         // outcome byte
			shorthandType byte         // outcome composite byte
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
			hdr: protocol.Header{Version: protocol.Version{1, 0}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b00010000, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "3.13",
			hdr: protocol.Header{Version: protocol.Version{3, 13}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x3D, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "JOIN",
			hdr: protocol.Header{Type: mt.Join},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0x4, 0},
			invalids: []error{},
		},
		{name: "15.15 MERGE_ACCEPT",
			hdr: protocol.Header{Version: protocol.Version{15, 15}, Type: mt.MergeAccept},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0xFF, 0x9, 0},
			invalids: []error{},
		},
		{name: "0.7 Get ID=15",
			hdr: protocol.Header{Version: protocol.Version{0, 7}, Type: mt.Get, ID: 15},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x7, 20, 15},
			invalids: []error{},
		},
		{name: "0.7 Get ID=MaxUint64",
			hdr: protocol.Header{Version: protocol.Version{0, 7}, Type: mt.Get, ID: math.MaxUint64},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x7, 20, math.MaxUint64},
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
			hdr: protocol.Header{Version: protocol.Version{3, 13}, Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0x3D, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},

		// invalids
		{name: "invalid major version",
			hdr: protocol.Header{Version: protocol.Version{31, 0}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b11110000, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMajor},
		},
		{name: "invalid major version with type",
			hdr: protocol.Header{Version: protocol.Version{32, 0}, Type: mt.Fault},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b0000, 0b00000001, 0},
			invalids: []error{protocol.ErrInvalidVersionMajor},
		},
		{name: "invalid minor version",
			hdr: protocol.Header{Version: protocol.Version{0, 255}},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0b00001111, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMinor},
		},
		{name: "invalid minor version shorthand",
			hdr: protocol.Header{Version: protocol.Version{0, 255}, Shorthand: true},
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
			hdr: protocol.Header{Shorthand: true, Type: mt.Increment, ID: 1},
			want: struct {
				version       byte
				shorthandType byte
				id            slims.NodeID
			}{0, 0b10001010, 1},
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
		if *hdr != zero {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(zero, *hdr))
		}
	})
	t.Run("single byte short read", func(t *testing.T) {
		var byt byte = 0b00100111 // 2.7
		hdr, err := protocol.Deserialize(bytes.NewReader([]byte{byt}))
		if err == nil {
			t.Error("expected a short read error, found none")
		}
		// ensure that hdr is empty
		want := protocol.Header{Version: protocol.VersionFromByte(byt)}
		if *hdr != want {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(want, *hdr))
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
			Version:   protocol.VersionFromByte(byt),
			Shorthand: true,
			Type:      mt.Hello,
		}
		if *hdr != want {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(want, *hdr))
		}
	})
	t.Run("missing ID", func(t *testing.T) {
		var byt byte = 0b01100110 // 6.6
		hdr, err := protocol.Deserialize(bytes.NewReader([]byte{byt, 0b00000011}))
		if err == nil {
			t.Error("expected a short read error, found none")
		}
		// ensure that hdr is empty
		want := protocol.Header{Version: protocol.VersionFromByte(byt), Type: mt.HelloAck}
		if *hdr != want {
			t.Error("incorrect header values after faulty deserialize", ExpectedActual(want, *hdr))
		}
	})

}

// Spins up a server to deserialize, ~~validate~~, serialize, and echo back whatever is sent to it.
func TestFullSend(t *testing.T) {
	// TODO t.Parallel
	var (
		echoServerID slims.NodeID = 1
		done         bool
	)
	// generate a server to echo data back
	pconn, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "[::0]:8080")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("listening @ ", pconn.LocalAddr())
	t.Cleanup(func() {
		pconn.Close()
		done = true
	})

	var pktbuf = make([]byte, slims.MaxPacketSize)
	go func() {
		for {
			rcvdN, senderAddr, err := pconn.ReadFrom(pktbuf)
			if rcvdN == 0 {
				if done {
					return
				}
				continue
			} else if err != nil {
				t.Error(err)
			}
			t.Logf("read %d bytes", rcvdN)

			// deserialize
			reqHdr, err := protocol.Deserialize(bytes.NewReader(pktbuf)) // TODO return header and body
			if err != nil {
				// respond with a FAULT
				b, serializeErr := protocol.Serialize(reqHdr.Version, false, mt.Fault, echoServerID)
				if serializeErr != nil {
					t.Error(serializeErr)
				}
				if _, err := pconn.WriteTo(append(b, []byte(err.Error())...), senderAddr); err != nil {
					t.Error(err)
				}
				continue
			}

			// validate
			if errs := reqHdr.Validate(); errs != nil {
				// respond with a FAULT
				b, serializeErr := protocol.Serialize(reqHdr.Version, false, mt.Fault, echoServerID)
				if serializeErr != nil {
					t.Error(serializeErr)
				}

				var errStr strings.Builder
				for _, err := range errs {
					errStr.WriteString(err.Error() + "\n")
				}

				if _, err := pconn.WriteTo(append(b, []byte(errStr.String()[:errStr.Len()-1])...), senderAddr); err != nil {
					t.Error(err)
				}
				continue
			}

			// re-serialize, but update ID
			reqHdr.ID = echoServerID
			respHdrB, err := reqHdr.Serialize()
			if err != nil {
				// respond with a FAULT
				b, serializeErr := protocol.Serialize(reqHdr.Version, false, mt.Fault, echoServerID)
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
		wantRespType mt.MessageType
	}{
		{"1.1, long, HELLO, ID0", &protocol.Header{Version: protocol.Version{1, 1}, Type: mt.Hello}, nil, mt.Hello},
		{"1.1, long, [unknown message type], ID0", &protocol.Header{Version: protocol.Version{1, 1}, Type: 50}, nil, mt.Fault},
		{"0.0, long, MERGE, ID22", &protocol.Header{Type: mt.Merge, ID: 22}, nil, mt.Merge},
		{"15.2, short, MERGE, ID22", &protocol.Header{Version: protocol.Version{15, 2}, Shorthand: true, Type: mt.Merge}, nil, mt.Merge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// serialize the header
			hdrB, err := tt.header.Serialize()
			if err != nil {
				t.Fatal(err)
			}

			// spawn a client to ping the server
			cli, err := net.DialUDP("udp", nil, &net.UDPAddr{Port: 8080})
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

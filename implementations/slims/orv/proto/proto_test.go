package proto_test

import (
	"bytes"
	"math"
	"network-bois-orv/implementations/slims/orv/proto"
	. "network-bois-orv/internal/testsupport"
	"slices"
	"testing"
)

// Tests that Serialize puts out the expected byte string fromm a given header struct.
// Serialize does not validate data in header, so errors can only come from a failure to write and thus are always fatal.
func TestHeader_SerializeWithValidate(t *testing.T) {
	tests := []struct {
		name     string
		hdr      proto.Header
		want     []byte // the byte string Serialize should return
		invalids uint   // the number of errors we expect .Validate() to return
	}{
		{"only hop limit", proto.Header{Version: proto.Version{0, 0}, HopLimit: 5}, []byte{0b0, 0b101, 0, 0, 0}, 1},
		{"1.1, hp5", proto.Header{Version: proto.Version{1, 1}, HopLimit: 5}, []byte{0b00010001, 0b101, 0, 0, 0}, 1},
		{"1.1, hp255", proto.Header{Version: proto.Version{1, 1}, HopLimit: 255}, []byte{0b00010001, 0b11111111, 0, 0, 0}, 1},
		{"15.15, hp255", proto.Header{Version: proto.Version{15, 15}, HopLimit: 255}, []byte{0b11111111, 0b11111111, 0, 0, 0}, 1},
		{"15.15, hp255, HELLO type", proto.Header{Version: proto.Version{15, 15}, HopLimit: 255, Type: proto.Hello}, []byte{0b11111111, 0b11111111, 0, 0, proto.Hello}, 0},
		{"HELLO_ACK type", proto.Header{Type: proto.HelloAck}, []byte{0, 0, 0, 0, proto.HelloAck}, 0},
		{"JOIN type", proto.Header{Type: proto.Join}, []byte{0, 0, 0, 0, proto.Join}, 0},
		{"JOIN_ACCEPT type", proto.Header{Type: proto.JoinAccept}, []byte{0, 0, 0, 0, proto.JoinAccept}, 0},
		{"JOIN_DENY type", proto.Header{Type: proto.JoinDeny}, []byte{0, 0, 0, 0, proto.JoinDeny}, 0},
		{"payload 20B, REGISTER type", proto.Header{PayloadLength: 20, Type: proto.Register}, []byte{0, 0, 0, 20, proto.Register}, 0},
		{"payload 20B, [overflow] type", proto.Header{PayloadLength: 20, Type: 250}, []byte{0, 0, 0, 20, 250}, 1},
		{"payload 65000B, REGISTER_ACCEPT type", proto.Header{PayloadLength: 65000, Type: proto.RegisterAccept}, []byte{0, 0, 0b11111101, 0b11101000, proto.RegisterAccept}, 0},

		{"bad major version",
			proto.Header{
				Version: proto.Version{33, 1},
			},
			[]byte{0b00010001, 0, 0, 0, 0}, // expect the 33 to be prefix-truncated to 1
			2,
		},
		{"payload too large",
			proto.Header{
				Version:       proto.Version{15, 1},
				HopLimit:      3,
				PayloadLength: math.MaxUint16,
				Type:          proto.VKHeartbeatFault,
			},
			[]byte{0b11110001, 3, math.MaxUint16 >> 8 & 0b11111111, math.MaxUint16 & 0b11111111, proto.VKHeartbeatFault},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if vErrors := tt.hdr.Validate(); len(vErrors) != int(tt.invalids) {
				t.Error("incorrect validation error count",
					ExpectedActual(tt.invalids, len(vErrors)),
					"errors: ", vErrors,
				)
			}

			if got, err := tt.hdr.Serialize(); err != nil {
				t.Fatal(err)
			} else if !slices.Equal(got, tt.want) {
				t.Error("bad byte string", ExpectedActual(tt.want, got))
			}
		})
	}
}

func TestHeader_Deserialize(t *testing.T) {
	tests := []struct {
		name string
		// the header to serialize.
		// The result of SerializeTo() is fed into SerializeFrom() to ensure it matches this original struct
		hdr proto.Header
		// the data to append to hdr after SerializeTo() but before SerializeFrom()
		body               []byte
		wantErr            bool
		wantBytesRemaining bool
	}{
		{"version + hop limit", proto.Header{Version: proto.Version{1, 1}, HopLimit: 16}, nil, false, false},
		{"version + hop limit + zero payload length", proto.Header{Version: proto.Version{1, 1}, HopLimit: 16}, nil, false, false},
		{"version + hop limit + zero payload length + type", proto.Header{Version: proto.Version{1, 1}, HopLimit: 16, Type: proto.HelloAck}, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr, err := (&tt.hdr).Serialize()
			if err != nil {
				t.Fatalf("failed to serialize header %v: %v", tt.hdr, err)
			}

			// wrap the header and body in a reader
			rd := bytes.NewReader(append(hdr, tt.body...))

			if err := tt.hdr.Deserialize(rd); (err != nil) != tt.wantErr {
				t.Errorf("Header.SerializeFrom() error = %v, wantErr %v", err, tt.wantErr)
			}
			if remaining := rd.Len(); (remaining != 0) != tt.wantBytesRemaining {
				t.Errorf("Expected bytes remaining? %v, actual bytes remaining: %v", tt.wantBytesRemaining, remaining)
			}
		})
	}

	// SerializeTo translates empty fields to zeros.
	// These tests manually construct the byte arrays s.t. SerializeFrom will early exist after reading all data in the buffer.
	t.Run("greedy population", func(t *testing.T) {
		type want struct {
			err           bool
			versionMajor  uint8
			versionMinor  uint8
			hopLimit      uint8
			payloadLength uint16
			typ           proto.MessageType
		}
		tests := []struct {
			name   string
			buffer []byte
			want   want
		}{
			{"empty buffer", nil, want{err: false}},
			{"version only", []byte{0b01000001}, want{err: false, versionMajor: 4, versionMinor: 1}},
			{"version + hop limit", []byte{0b01000001, 15}, want{
				err: false, versionMajor: 4, versionMinor: 1, hopLimit: 15,
			}},
			{"version + hop limit + payload length", []byte{0b01000001, 15, 64023 >> 8, 64023 & 0xFF}, want{
				err: false, versionMajor: 4, versionMinor: 1, hopLimit: 15, payloadLength: 64023,
			}},
			{"version + hop limit + payload length + type", []byte{0b01000001, 15, 64023 >> 8, 64023 & 0xFF, proto.Increment}, want{
				err: false, versionMajor: 4, versionMinor: 1, hopLimit: 15, payloadLength: 64023, typ: proto.Increment,
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var result proto.Header
				if err := result.Deserialize(bytes.NewReader(tt.buffer)); err != nil {
					t.Error(err)
				}
				if result.Version.Major != tt.want.versionMajor {
					t.Errorf("bad major version. Expected %b, got %b", tt.want.versionMajor, result.Version.Major)
				} else if result.Version.Minor != tt.want.versionMinor {
					t.Errorf("bad major version. Expected %b, got %b", tt.want.versionMinor, result.Version.Minor)
				} else if result.HopLimit != tt.want.hopLimit {
					t.Errorf("bad hop limit. Expected %b, got %b", tt.want.hopLimit, result.HopLimit)
				} else if result.PayloadLength != tt.want.payloadLength {
					t.Errorf("bad payload length. Expected %b, got %b", tt.want.payloadLength, result.PayloadLength)
				} else if result.Type != tt.want.typ {
					t.Errorf("bad message type. Expected %b, got %b", tt.want.typ, result.Type)
				}
			})
		}
	})
}

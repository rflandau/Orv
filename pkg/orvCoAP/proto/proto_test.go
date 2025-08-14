package proto_test

import (
	"bytes"
	"math"
	"network-bois-orv/pkg/orvCoAP/proto"
	"reflect"
	"testing"
)

func TestHeader_SerializeTo(t *testing.T) {
	tests := []struct {
		name    string
		hdr     proto.Header
		want    []byte
		wantErr bool
	}{
		{"invalid hop limit", proto.Header{Version: proto.Version{0, 0}, HopLimit: 0}, nil, true},
		{"invalid payload length", proto.Header{Version: proto.Version{0, 0}, HopLimit: 3, PayloadLength: math.MaxUint16}, nil, true},
		{"only hop limit", proto.Header{Version: proto.Version{0, 0}, HopLimit: 5}, []byte{0b0, 0b101, 0, 0, 0}, false},
		{"1.1, hp5", proto.Header{Version: proto.Version{1, 1}, HopLimit: 5}, []byte{0b00010001, 0b101, 0, 0, 0}, false},
		{"1.1, hp255", proto.Header{Version: proto.Version{1, 1}, HopLimit: 255}, []byte{0b00010001, 0b11111111, 0, 0, 0}, false},
		{"15.15, hp255", proto.Header{Version: proto.Version{15, 15}, HopLimit: 255}, []byte{0b11111111, 0b11111111, 0, 0, 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.hdr.Serialize()
			if (err != nil) != tt.wantErr {
				t.Errorf("Header.SerializeTo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Header.SerializeTo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeader_SerializeFrom(t *testing.T) {
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

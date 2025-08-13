package proto_test

import (
	"bytes"
	"network-bois-orv/pkg/orvCoAP/proto"
	"reflect"
	"testing"
)

func TestHeader_SerializeTo(t *testing.T) {
	type fields struct {
		Version  proto.Version
		HopLimit uint8
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{"invalid hop limit", fields{Version: proto.Version{0, 0}, HopLimit: 0}, nil, true},
		{"only hop limit", fields{Version: proto.Version{0, 0}, HopLimit: 5}, []byte{0b0, 0b101}, false},
		{"1.1, hp5", fields{Version: proto.Version{1, 1}, HopLimit: 5}, []byte{0b00010001, 0b101}, false},
		{"1.1, hp255", fields{Version: proto.Version{1, 1}, HopLimit: 255}, []byte{0b00010001, 0b11111111}, false},
		{"15.15, hp255", fields{Version: proto.Version{15, 15}, HopLimit: 255}, []byte{0b11111111, 0b11111111}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr := &proto.Header{
				Version:  tt.fields.Version,
				HopLimit: tt.fields.HopLimit,
			}
			got, err := hdr.SerializeTo()
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
		name               string
		hdr                proto.Header
		body               []byte
		wantErr            bool
		wantBytesRemaining bool
	}{
		{"basic", proto.Header{Version: proto.Version{1, 1}, HopLimit: 8}, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr, err := (&tt.hdr).SerializeTo()
			if err != nil {
				t.Fatalf("failed to serialize header %v: %v", tt.hdr, err)
			}

			// wrap the header and body in a reader
			rd := bytes.NewReader(append(hdr, tt.body...))

			if err := tt.hdr.SerializeFrom(rd); (err != nil) != tt.wantErr {
				t.Errorf("Header.SerializeFrom() error = %v, wantErr %v", err, tt.wantErr)
			}
			if remaining := rd.Len(); (remaining != 0) != tt.wantBytesRemaining {
				t.Errorf("Expected bytes remaining? %v, actual bytes remaining: %v", tt.wantBytesRemaining, remaining)
			}
		})
	}
}

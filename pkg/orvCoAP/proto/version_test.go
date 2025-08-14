// Package proto contains tools for interacting with the L5 orv header.
// Includes structs that can be composed into a fixed header; you should never have to interact with the raw bits or endian-ness of the header.
package proto_test

import (
	"network-bois-orv/pkg/orvCoAP/proto"
	"testing"
)

// Tests creating a new version from uint8 major and minors and that the byte .Byte() returns is as expected.
func TestVersion(t *testing.T) {
	type args struct {
		major uint8
		minor uint8
	}
	type want struct {
		B   byte
		err bool
	}
	tests := []struct {
		name string
		args args
		want want
	}{
		{"invalid major", args{16, 1}, want{0b0, true}},
		{"invalid minor", args{2, 255}, want{0b0, true}},
		{"0.0", args{0, 0}, want{0b00000000, false}},
		{"0.10", args{0, 10}, want{0b00001010, false}},
		{"0.10", args{3, 0}, want{0b00110000, false}},
		{"2.1", args{2, 1}, want{0b00100001, false}},
		{"15.15", args{15, 15}, want{0b11111111, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := proto.NewVersion(tt.args.major, tt.args.minor)
			if (err != nil) != tt.want.err {
				t.Errorf("NewVersion() error = %v, wantErr %v", err, tt.want.err)
				return
			}
			if v.Byte() != tt.want.B {
				t.Errorf("NewVersion() = %v, want %v", v, tt.want)
			}
		})
	}
}

// Tests parsing out Version from a byte.
func TestVersionFromByte(t *testing.T) {
	tests := []struct {
		name string
		b    byte
		want proto.Version
	}{
		{"0.0", 0b00000000, proto.Version{0, 0}},
		{"0.0", 0b0, proto.Version{0, 0}},
		{"0.1", 0b1, proto.Version{0, 1}},
		{"1.0", 0b00010000, proto.Version{1, 0}},
		{"1.1", 0b00010001, proto.Version{1, 1}},
		{"15.15", 0b11111111, proto.Version{15, 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proto.VersionFromByte(tt.b)
			if got.Major != tt.want.Major || got.Minor != tt.want.Minor {
				t.Fatalf("expected %v, actual %v", tt.want, got)
			}
		})
	}
}

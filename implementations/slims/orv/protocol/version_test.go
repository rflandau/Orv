package protocol

import (
	"maps"
	"slices"
	"strconv"
	"testing"

	. "github.com/rflandau/Orv/internal/testsupport"
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
			v, err := NewVersion(tt.args.major, tt.args.minor)
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
		want Version
	}{
		{"0.0", 0b00000000, Version{0, 0}},
		{"0.0", 0b0, Version{0, 0}},
		{"0.1", 0b1, Version{0, 1}},
		{"1.0", 0b00010000, Version{1, 0}},
		{"1.1", 0b00010001, Version{1, 1}},
		{"15.15", 0b11111111, Version{15, 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := VersionFromByte(tt.b)
			if got.Major != tt.want.Major || got.Minor != tt.want.Minor {
				t.Fatalf("expected %v, actual %v", tt.want, got)
			}
		})
	}
}

// Tests that VersionsSupportedAsBytes stably sorts highest to lowest.
// Because we use a package variable, we have to change it manually here, so cannot run these in parallel.
func TestVersionsSupportedAsBytes(t *testing.T) {
	tests := []struct {
		name     string
		version  map[uint8][]uint8
		expected []byte
	}{
		{"multiple versions", map[uint8][]uint8{1: {2}, 2: {1, 5}}, []byte{0b00100101, 0b00100001, 0b00010010}},
		{"single version", map[uint8][]uint8{1: {2}}, []byte{0b00010010}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supportedVersions = tt.version

			vs := VersionsSupportedAsBytes()
			if slices.Compare(vs, tt.expected) != 0 {
				t.Fatal(ExpectedActual(tt.expected, vs))
			}
		})
	}

}

func TestVersionSupported(t *testing.T) {
	vs := VersionsSupported()
	if !maps.EqualFunc(supportedVersions, vs, func(a, b []byte) bool {
		return slices.Equal(a, b)
	}) {
		t.Fatal(ExpectedActual(supportedVersions, vs))
	}

}
func TestIsVersionSupported(t *testing.T) {
	supportedVersions = map[uint8][]uint8{
		1:  {1, 2},
		15: {15},
	}
	test := []struct {
		version   Version
		supported bool
	}{
		{Version{1, 1}, true},
		{Version{1, 2}, true},
		{Version{1, 3}, false},
		{Version{1, 7}, false},
		{Version{2, 1}, false},
		{Version{15, 15}, true},
	}
	for i, tt := range test {
		t.Run(strconv.FormatInt(int64(i), 10), func(t *testing.T) {
			if IsVersionSupported(tt.version) != tt.supported {
				t.Fatal(ExpectedActual(IsVersionSupported(tt.version), tt.supported))
			}
		})
	}

}

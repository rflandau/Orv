package version_test

import (
	"slices"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
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
			t.Parallel()
			v, err := version.New(tt.args.major, tt.args.minor)
			if (err != nil) != tt.want.err {
				t.Errorf("NewVersion() error = %v, wantErr %v", err, tt.want.err)
				return
			}
			if v.Byte() != tt.want.B {
				t.Errorf("NewVersion() = %v, want %v", v, tt.want)
			}
		})
	}
	t.Run("FromByte", func(t *testing.T) {
		tests := []struct {
			name string
			b    byte
			want version.Version
		}{
			{"0.0", 0b00000000, version.Version{0, 0}},
			{"0.0", 0b0, version.Version{0, 0}},
			{"0.1", 0b1, version.Version{0, 1}},
			{"1.0", 0b00010000, version.Version{1, 0}},
			{"1.1", 0b00010001, version.Version{1, 1}},
			{"15.15", 0b11111111, version.Version{15, 15}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := version.FromByte(tt.b)
				if got.Major != tt.want.Major || got.Minor != tt.want.Minor {
					t.Fatalf("expected %v, actual %v", tt.want, got)
				}
			})
		}
	})
}

// Tests that Set.AsBytes stably sorts highest to lowest.
func TestSet(t *testing.T) {
	tests := []struct {
		name                     string
		set                      version.Set
		expected                 []byte            // expected is, itself, expected to be sorted
		supportedVersions        []version.Version // versions to check this map for support of; maps to expectedSupported
		expectedSupported        []bool            // maps to supportedVersions
		expectedHighestSupported version.Version
	}{
		{"multiple versions, pre-sorted",
			version.NewSet(version.Version{1, 2}, version.Version{2, 1}, version.Version{2, 5}), []byte{0b00100101, 0b00100001, 0b00010010},
			[]version.Version{{1, 2}, {2, 1}}, []bool{true, true},
			version.Version{2, 5},
		},
		{"multiple versions, unsorted",
			version.NewSet(version.Version{2, 1}, version.Version{1, 2}, version.Version{2, 5}), []byte{0b00100101, 0b00100001, 0b00010010},
			[]version.Version{{1, 2}, {2, 1}}, []bool{true, true},
			version.Version{2, 5},
		},
		{"single version",
			version.NewSet(version.Version{0, 1}), []byte{0b00000001},
			[]version.Version{{0, 0}, {0, 1}, {5, 15}}, []bool{false, true, false},
			version.Version{0, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if slices.Compare(tt.set.AsBytes(), tt.expected) != 0 {
				t.Fatal(ExpectedActual(tt.expected, tt.set.AsBytes()))
			}
			// sanity check
			if len(tt.supportedVersions) != len(tt.expectedSupported) {
				t.Fatalf("supportedVersions (%d) count must match expectedSupported count (%d)", len(tt.supportedVersions), len(tt.expectedSupported))
			}
			for i := range tt.supportedVersions {
				if supported := tt.set.Supports(tt.supportedVersions[i]); supported != tt.expectedSupported[i] {
					t.Fatalf("version %v reported as supported[%v], but was expected to report as supported[%v]", tt.supportedVersions[i], supported, tt.expectedSupported[i])
				}
			}
			if tt.expectedHighestSupported != tt.set.HighestSupported() {
				t.Fatal(ExpectedActual(tt.set.HighestSupported(), tt.expectedHighestSupported))
			}
		})
	}
}

package slims_test

import (
	"errors"
	"strconv"
	"testing"

	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
)

// Simple series of tests just to ensure the basic logic is in place
func TestErrno_Is(t *testing.T) {
	tests := []struct {
		src    error
		target error
		is     bool
	}{
		{slims.Errno{}, nil, false},
		{slims.Errno{AdditionalInfo: "FLÄISUM"}, slims.Errno{}, true},
		{slims.Errno{Num: pb.Fault_MALFORMED_BODY}, slims.Errno{}, false},
		{slims.Errno{Num: pb.Fault_MALFORMED_ADDRESS}, slims.Errno{Num: pb.Fault_MALFORMED_ADDRESS}, true},
		{slims.Errno{Num: pb.Fault_MALFORMED_ADDRESS}, slims.Errno{Num: pb.Fault_MALFORMED_ADDRESS, AdditionalInfo: "FLÄSHYN"}, true},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if match := errors.Is(tt.src, tt.target); match != tt.is {
				t.Error(ExpectedActual(tt.is, match))
			}
		})
	}

}

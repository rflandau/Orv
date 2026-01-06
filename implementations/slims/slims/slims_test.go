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

func TestFormatFault(t *testing.T) {
	t.Run("nil param", func(t *testing.T) {
		if err := slims.FormatFault(nil); err != nil {
			t.Fatal("FormatFault returned non-nil error when given nil parameter")
		}
	})

	t.Run("IsErrno_NoAdditionalInfo", func(t *testing.T) {
		errno := pb.Fault_MALFORMED_BODY

		// generate a test fault
		f := &pb.Fault{
			Original: pb.MessageType_DEREGISTER,
			Errno:    errno,
		}
		// check that FormatFault correctly returns an Errno error
		err := slims.FormatFault(f)
		if err == nil {
			t.Fatal("Returned a nil error")
		} else if !errors.Is(err, slims.Errno{Num: errno}) {
			t.Fatal("Returned error is not the correct errno type")
		}
	})
	t.Run("IsErrno_WithAdditionalInfo", func(t *testing.T) {
		errno := pb.Fault_UNSPECIFIED

		// generate a test fault
		ai := "an unspecified error occurred"
		f := &pb.Fault{
			Original:       pb.MessageType_UNKNOWN,
			Errno:          errno,
			AdditionalInfo: &ai,
		}
		// check that FormatFault correctly returns an Errno error
		err := slims.FormatFault(f)
		if err == nil {
			t.Fatal("Returned a nil error")
		} else if !errors.Is(err, slims.Errno{Num: errno}) {
			t.Fatal("Returned error is not the correct errno type (sans AI)")
		} else if !errors.Is(err, slims.Errno{Num: errno, AdditionalInfo: ai}) {
			t.Fatal("Returned error is not the correct errno type (with AI)")
		}
	})
}

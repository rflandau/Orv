// Package slims is the parent package of the Orv Slims implementation.
// It contains child packages vk (vault keeper implementation), proto (L5 Orv header handling), and client (static subroutines for interacting with a vault).
// Child packages are mostly self-contained, the Orv parent package provides the few shared utilities.
package slims

import (
	"errors"
	"strconv"
	"strings"

	"github.com/rflandau/Orv/implementations/slims/slims/pb"
)

// NodeID is the VK or Leaf's unique identifier
type NodeID = uint64

// MaxPacketSize specifies the buffer size used to hold UDP payloads.
// UDP can theoretically support payloads nearing 65535 bytes, but 1500B is a common MTU and Slims assumes that each Orv message fits into a single packet.
// Thankfully for our allocator, Orv packets should easily fit in a thousand bytes, if not much less.
// The specific maximum is set arbitrarily and can be changed arbitrarily (at the cost of requiring more heap memory for packet processing and a greater MTU for transmission).
const MaxPacketSize uint16 = 1024

var ErrNilCtx = errors.New("do not pass nil contexts; use context.TODO or context.Background instead")

// ErrUnexpectedResponseType indicates that a response was of the wrong message type.
type ErrUnexpectedResponseType string

func (e ErrUnexpectedResponseType) Error() string {
	return "unexpected response type: " + string(e)
}

// FormatFault is a helper function used to format a fault message into an Errno type error.
func FormatFault(f *pb.Fault) error {
	if f == nil {
		return nil
	}
	err := Errno{Num: f.Errno}
	if f.AdditionalInfo != nil {
		err.AdditionalInfo = *f.AdditionalInfo
	}

	return err
}

// Errno provides an error type for and way to compare errnos.
// It is more accurate than ErrContainsErrno, but may not be implemented everywhere.
type Errno struct {
	Num            pb.Fault_Errnos
	AdditionalInfo string
}

func (e Errno) Error() string {
	base := strconv.FormatInt(int64(e.Num), 10)
	if e.AdditionalInfo != "" {
		return base + " (" + e.AdditionalInfo + ")"
	}
	return base
}

// Is checks if the given error's errno matches ours.
// It does not care about additionalInfo.
func (e Errno) Is(target error) bool {
	if target == nil {
		return false
	}
	targetErrno, ok := target.(Errno)
	if !ok {
		return false
	}
	return targetErrno.Num == e.Num
}

// ErrContainsErrno checks if the given error contains given errno.
// This check is coarse and should be replaced by a new error type and errors.Is()... eventually.
func ErrContainsErrno(err error, errno pb.Fault_Errnos) bool {
	if err == nil {
		return false
	}
	numStr := strconv.FormatInt(int64(errno.Number()), 10)
	return strings.Contains(err.Error(), numStr)
}

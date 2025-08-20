// Package testsupport is an internal-only package that provides utilities for testing uniformity.
package testsupport

import (
	"context"
	"fmt"
	"time"

	"github.com/plgd-dev/go-coap/v3/udp"
)

// ExpectedActual returns a newline-prefixed string comparing the expected result to the actual result.
// Should be used to add clarity to unit test error messages.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected: '%v'\n\tActual: '%v'", expected, actual)
}

// CoApPing is a helper function that sends a ping to the given address.
func CoAPPing(addr string, timeout time.Duration) error {
	// ping the server
	conn, err := udp.Dial(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		return err
	}
	return nil
}

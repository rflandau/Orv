// Package testsupport is an internal-only package that provides utilities for testing uniformity.
package testsupport

import "fmt"

// ExpectedActual returns a newline-prefixed string comparing the expected result to the actual result.
// Should be used to add clarity to unit test error messages.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected: '%v'\n\tActual: '%v'", expected, actual)
}

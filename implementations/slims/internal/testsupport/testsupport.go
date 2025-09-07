// Package testsupport is an internal-only package that provides utilities for testing uniformity.
package testsupport

import (
	"fmt"
	"maps"
	"sync"
)

// ExpectedActual returns a newline-prefixed string comparing the expected result to the actual result.
// Should be used to add clarity to unit test error messages.
func ExpectedActual[T any](expected, actual T) string {
	return fmt.Sprintf("\n\tExpected: '%v'\n\tActual: '%v'", expected, actual)
}

// SlicesUnorderedEqual compares the elements of the given slices for equality and equal count without taking order of the elements into account.
func SlicesUnorderedEqual[T comparable](a []T, b []T) bool {
	// convert each slice into a map of key --> count
	var wg sync.WaitGroup

	am := make(map[T]uint)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, k := range a {
			am[k] += 1
		}
	}()

	bm := make(map[T]uint)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, k := range b {
			bm[k] += 1
		}
	}()

	wg.Wait()
	return maps.Equal(am, bm)
}

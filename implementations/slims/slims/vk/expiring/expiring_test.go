package expiring_test

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	. "github.com/rflandau/Orv/implementations/slims/internal/testsupport"
	"github.com/rflandau/Orv/implementations/slims/slims/vk/expiring"
)

func TestTable(t *testing.T) {
	t.Run("prune on timeout", func(t *testing.T) {
		tbl := expiring.New[int, float64]()

		k, timeout := 0, 5*time.Millisecond
		tbl.Store(k, 1.1, timeout)
		time.Sleep(timeout + time.Millisecond)
		if v, found := tbl.Load(k); found {
			t.Errorf("k/v %d/%v should have expired, but was found", k, v)
		}

		k, timeout = 0, 100*time.Millisecond
		tbl.Store(k, -1111.2222, timeout)
		time.Sleep(timeout + time.Millisecond)
		if v, found := tbl.Load(k); found {
			t.Errorf("k/v %d/%v should have expired, but was found", k, v)
		}

		k, timeout = -650493712, 20*time.Millisecond
		tbl.Store(k, 1.1, timeout)
		time.Sleep(timeout + time.Millisecond)
		if v, found := tbl.Load(k); found {
			t.Errorf("k/v %d/%v should have expired, but was found", k, v)
		}
	})

	t.Run("no prune prior to timeout", func(t *testing.T) {
		tbl := expiring.New[string, bool]()

		tests := []struct {
			k    string
			v    bool
			time time.Duration
		}{
			{"Cipher Pata", true, 150 * time.Millisecond},
			{"Godslayer's Seal", true, 10 * time.Millisecond},
			{"O, Flame!", false, 5 * time.Millisecond},
		}

		for i, tt := range tests {
			t.Run(strconv.FormatInt(int64(i), 10), func(t *testing.T) {
				tbl.Store(tt.k, tt.v, tt.time)
				checkLoad(t, tbl, tt.k, true, tt.v)
				// unclear how much time has elapsed since original store, so sleep conservatively
				time.Sleep((tt.time * 2) / 3)
				checkLoad(t, tbl, tt.k, true, tt.v)
				time.Sleep(tt.time/3 + time.Millisecond)
				checkLoad(t, tbl, tt.k, false, tt.v)

			})
		}

		t.Run("millisecond before prune", func(t *testing.T) {
			key := "Antspur Rapier"
			// insert a value and check it moments before pruning
			tbl.Store(key, true, 10*time.Millisecond)
			time.Sleep(9 * time.Millisecond)
			// NOTE(rlandau): we are close enough that instruction interleaving could cause fetching to fail, so this test may need to be tweaked on other machines
			checkLoad(t, tbl, key, true, true)
		})
	})

	t.Run("reset timer on new store", func(t *testing.T) {
		tbl := expiring.New[*int, string]()
		key, val := 151, "Wing of Astel"

		tbl.Store(&key, val, 5*time.Millisecond)
		checkLoad(t, tbl, &key, true, val)
		tbl.Store(&key, val, 20*time.Millisecond)
		time.Sleep(5 * time.Millisecond)
		checkLoad(t, tbl, &key, true, val)
		time.Sleep(16 * time.Millisecond)
		checkLoad(t, tbl, &key, false, val)
	})

	t.Run("delete elements", func(t *testing.T) {
		tbl := expiring.New[string, string]()
		// insert and delete a key
		key, val := "Comet Azur", "Azur Staff"
		tbl.Store(key, val, 40*time.Millisecond)
		if !tbl.Delete(key) {
			t.Fatalf("failed to delete key='%v': not found", key)
		}
		checkLoad(t, tbl, key, false, val)
		// delete a key that does not exist
		if tbl.Delete("Aomet Czur") {
			t.Fatal("successfully deleted non-existent key")
		}
	})
	t.Run("reset", func(t *testing.T) {
		var (
			k = struct{ a int }{32}
			v = 3.14
		)

		tbl := expiring.New[struct{ a int }, float64]()
		// insert a new key
		tbl.Store(k, v, 20*time.Millisecond)
		// test the value
		time.Sleep(10 * time.Millisecond)
		checkLoad(t, tbl, k, true, v)
		// reset it before it expires
		if !tbl.Refresh(k, 40*time.Millisecond) {
			t.Fatal("failed to refresh value prior to original expiry: not found")
		}
		time.Sleep(30 * time.Millisecond)
		checkLoad(t, tbl, k, true, v)
		time.Sleep(11 * time.Millisecond)
		checkLoad(t, tbl, k, false, v)
		// refresh a non-existent key
		if tbl.Refresh(struct{ a int }{1}, 10000000) {
			t.Fatal("successfully refreshed non-existent key")
		}
	})
	t.Run("additional clean up functions", func(t *testing.T) {
		var (
			cleanupBuf         = []int{}
			expectedCleanupBuf = []int{1, -1, -2, 2, -1, -2, 3, -1, -2}
			mu                 sync.Mutex
		)
		tbl := expiring.New[int, int]()
		tbl.Store(-1, -2, 50*time.Millisecond, func(k, v int) {
			mu.Lock()
			defer mu.Unlock()
			cleanupBuf = append(cleanupBuf, 1)
			cleanupBuf = append(cleanupBuf, k)
			cleanupBuf = append(cleanupBuf, v)
		}, func(k, v int) {
			mu.Lock()
			defer mu.Unlock()
			cleanupBuf = append(cleanupBuf, 2)
			cleanupBuf = append(cleanupBuf, k)
			cleanupBuf = append(cleanupBuf, v)
		}, func(k, v int) {
			mu.Lock()
			defer mu.Unlock()
			cleanupBuf = append(cleanupBuf, 3)
			cleanupBuf = append(cleanupBuf, k)
			cleanupBuf = append(cleanupBuf, v)
		})
		time.Sleep(55 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if slices.Compare(cleanupBuf, expectedCleanupBuf) != 0 {
			t.Fatal("clean up functions did not execute properly", ExpectedActual(expectedCleanupBuf, cleanupBuf))
		}
	})
}

func TestTable_Range(t *testing.T) {
	in := map[string]int{
		"icerind hatchet": 1,
		"backhand blade":  -2,
		"misericorde":     1000,
		"reduvia":         11,
	}
	t.Run("all items", func(t *testing.T) {
		// create an expiring table from input
		tbl := expiring.New[string, int]()
		for k, v := range in {
			tbl.Store(k, v, 3*time.Second)
		}

		// rebuild in by ranging over in
		out := make(map[string]int)
		tbl.RangeLocked(func(s string, i int) bool {
			out[s] = i
			return true
		})

		if !maps.Equal(in, out) {
			t.Fatal("input and output maps do not match", ExpectedActual(in, out))
		}
	})
	t.Run("early exit", func(t *testing.T) {
		// create table
		tbl := expiring.New[string, int]()
		for k, v := range in {
			tbl.Store(k, v, 3*time.Second)
		}

		// early exit after 2 calls
		var callCount uint
		out := make(map[string]int)
		tbl.RangeLocked(func(s string, i int) bool {
			out[s] = i
			callCount += 1
			return callCount < 2
		})

		// ensure we have exactly 2 elements in our output map
		if len(out) != int(callCount) {
			t.Fatal("incorrect range call counts.", ExpectedActual(int(callCount), len(out)))
		}
	})
}

// tests the load returns the expected value and found state.
// Value is only checked if an element was found.
func checkLoad[key_t comparable, val_t comparable](t *testing.T, tbl *expiring.Table[key_t, val_t], key key_t, expectedFound bool, expectedVal val_t) {
	t.Helper()
	v, found := tbl.Load(key)
	if found != expectedFound {
		t.Error("incorrect found", ExpectedActual(expectedFound, found))
	}
	if found && (v != expectedVal) { // only check value if one was actually found
		t.Error("incorrect value retrieved", ExpectedActual(expectedVal, v))
	}
}

func TestTable_CompareAndSwap(t *testing.T) {
	expireTime := 10 * time.Second // functionally disable expire times
	t.Run("sequential swaps", func(t *testing.T) {
		// TODO
	})

	// Checks that, when multiple goroutines hit CompareAndSwap simultaneous, the ending value is consistent to the key-pair.
	t.Run("parallel swaps", func(t *testing.T) {
		pairCount := rand.UintN(50) + 2
		key := randomdata.SillyName()              // the key every goro is trying to swap in
		values := make(map[uint]string, pairCount) // goro index -> random value

		for i := range pairCount { // map each goroutine index to a random value
			values[i] = randomdata.SillyName()
		}

		t.Run("empty value swaps", func(t *testing.T) {
			tbl := expiring.New[string, string]()
			var (
				zero         string
				swapperIndex atomic.Uint64
				wg           sync.WaitGroup
			)
			// spin out a goroutine to try and swap in each value concurrently
			for idx, v := range values {
				wg.Add(1)
				go func(index uint, value string) {
					defer wg.Done()
					// attempt to swap in our value
					swapped := tbl.CompareAndSwap(key, zero, value, 10*time.Second)
					// if we succeeded, set our index to the swapper index
					if swapped {
						if !swapperIndex.CompareAndSwap(0, uint64(index)) {
							// someone beat us here, which means two goros successfully swapped with CompareAndSwap
							panic("failed to save off swapper index because another goroutine had already swapped, but we managed to swap the table.")
						}
					}
				}(idx, v)
			}

			wg.Wait()
			// if we got here, no goroutines panic, so we just need to check that a value was actually set
			swappedIn, found := tbl.Load(key)
			if !found {
				t.Fatalf("failed to find key '%v', which means no swaps succeeded", key)
			}
			swapperIdx := uint(swapperIndex.Load())
			swappersValue, found := values[swapperIdx]
			if !found {
				t.Fatalf("no swapper value associated to index %d", swapperIdx)
			}
			if swappedIn != swappersValue {
				t.Fatalf("value swapped into the table (%v) does not equal the value associated to the swapper index (%v: %v)", swappedIn, swapperIdx, swappersValue)
			}
		})

		// try existing swaps
		t.Run("existing value swaps", func(t *testing.T) {
			tbl := expiring.New[string, string]()
			var (
				swapperIndex     atomic.Uint64
				wg               sync.WaitGroup
				preexistingValue = randomdata.Adjective()
			)
			// store the key with a default value
			tbl.Store(key, preexistingValue, expireTime)

			// spin out goroutines to try and swap in each value concurrently
			for idx, v := range values {
				wg.Add(1)
				go func(index uint, value string) {
					defer wg.Done()
					// if we succeed in swapping, set our index to the swapper index
					if tbl.CompareAndSwap(key, preexistingValue, value, expireTime) {
						if !swapperIndex.CompareAndSwap(0, uint64(index)) {
							// someone beat us here, which means two goros successfully swapped with CompareAndSwap
							panic(fmt.Sprintf("%d: failed to save off swapper index because another goroutine had already swapped, but we managed to swap the table. (swappedIndex: %d)", index, swapperIndex.Load()))
						}
					}
				}(idx, v)
			}

			wg.Wait()
			// if we got here, no goroutines panic, so we just need to check that a value was actually set
			swappedIn, found := tbl.Load(key)
			if !found {
				t.Fatalf("failed to find key '%v', which means no swaps succeeded", key)
			}
			swapperIdx := uint(swapperIndex.Load())
			swappersValue, found := values[swapperIdx]
			if !found {
				t.Fatalf("no swapper value associated to index %d", swapperIdx)
			}
			if swappedIn != swappersValue {
				t.Fatalf("value swapped into the table (%v) does not equal the value associated to the swapper index (%v: %v)", swappedIn, swapperIdx, swappersValue)
			}
		})
	})
}

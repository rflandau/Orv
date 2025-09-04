package expiring_test

import (
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims/vk/expiring"
	. "github.com/rflandau/Orv/internal/testsupport"
)

func TestTable(t *testing.T) {
	t.Run("prune on timeout", func(t *testing.T) {
		tbl := expiring.Table[int, float64]{}

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
		tbl := expiring.Table[string, bool]{}

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
				checkLoad(t, &tbl, tt.k, true, tt.v)
				// unclear how much time has elapsed since original store, so sleep conservatively
				time.Sleep((tt.time * 2) / 3)
				checkLoad(t, &tbl, tt.k, true, tt.v)
				time.Sleep(tt.time/3 + time.Millisecond)
				checkLoad(t, &tbl, tt.k, false, tt.v)

			})
		}

		t.Run("millisecond before prune", func(t *testing.T) {
			key := "Antspur Rapier"
			// insert a value and check it moments before pruning
			tbl.Store(key, true, 10*time.Millisecond)
			time.Sleep(9 * time.Millisecond)
			// NOTE(rlandau): we are close enough that instruction interleaving could cause fetching to fail, so this test may need to be tweaked on other machines
			checkLoad(t, &tbl, key, true, true)
		})
	})

	t.Run("reset timer on new store", func(t *testing.T) {
		tbl := expiring.Table[*int, string]{}
		key, val := 151, "Wing of Astel"

		tbl.Store(&key, val, 5*time.Millisecond)
		checkLoad(t, &tbl, &key, true, val)
		tbl.Store(&key, val, 20*time.Millisecond)
		time.Sleep(5 * time.Millisecond)
		checkLoad(t, &tbl, &key, true, val)
		time.Sleep(16 * time.Millisecond)
		checkLoad(t, &tbl, &key, false, val)
	})

	t.Run("delete elements", func(t *testing.T) {
		tbl := expiring.Table[string, string]{}
		// insert and delete a key
		key, val := "Comet Azur", "Azur Staff"
		tbl.Store(key, val, 40*time.Millisecond)
		if !tbl.Delete(key) {
			t.Fatalf("failed to delete key='%v': not found", key)
		}
		checkLoad(t, &tbl, key, false, val)
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

		tbl := expiring.Table[struct{ a int }, float64]{}
		// insert a new key
		tbl.Store(k, v, 20*time.Millisecond)
		// test the value
		time.Sleep(10 * time.Millisecond)
		checkLoad(t, &tbl, k, true, v)
		// reset it before it expires
		if !tbl.Refresh(k, 40*time.Millisecond) {
			t.Fatal("failed to refresh value prior to original expiry: not found")
		}
		time.Sleep(30 * time.Millisecond)
		checkLoad(t, &tbl, k, true, v)
		time.Sleep(11 * time.Millisecond)
		checkLoad(t, &tbl, k, false, v)
		// refresh a non-existent key
		if tbl.Refresh(struct{ a int }{1}, 10000000) {
			t.Fatal("successfully refreshed non-existent key")
		}
	})
	t.Run("additional clean up functions", func(t *testing.T) {
		var (
			cleanupBuf         = []int{}
			expectedCleanupBuf = []int{1, 2, 3}
			mu                 sync.Mutex
		)
		tbl := expiring.Table[string, string]{}
		tbl.Store("key", "value", 50*time.Millisecond, func() {
			mu.Lock()
			defer mu.Unlock()
			cleanupBuf = append(cleanupBuf, 1)
		}, func() {
			mu.Lock()
			defer mu.Unlock()
			cleanupBuf = append(cleanupBuf, 2)
		}, func() {
			mu.Lock()
			defer mu.Unlock()
			cleanupBuf = append(cleanupBuf, 3)
		})
		time.Sleep(55 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if slices.Compare(cleanupBuf, expectedCleanupBuf) != 0 {
			t.Fatal("clean up functions did not execute properly", ExpectedActual(expectedCleanupBuf, cleanupBuf))
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

// Package expiring introduces tables with the ability to prune their own elements.
package expiring

import (
	"fmt"
	"sync"
	"time"
)

// wrapped value with an expiration timer attached
type timedV[value_t any] struct {
	val value_t
	exp *time.Timer // expiring timer to clear this function out of the table
}

// A Table is basically a syncmap, but elements prune themselves after their duration elapses.
// The zero value is ready for immediate use.
//
// NOTE(rlandau): Tables should only be passed by reference due to underlying mutex use.
//
// NOTE(rlandau): accessing elements AT their expiration time is, by its very nature, a race.
// If a timer has not expired, then its associated data is guaranteed to not have been pruned. The inverse is not guaranteed.
//
// NOTE(rlandau): using a syncmap under the hood makes development really easy, but means that Tables do not retain the atomic nature of syncmaps.
// This implementation is vulnerable to interleaved commands from multiple .Store()s; a more robust implementation would use a normal map to hold data and a second table to associate keys to mutexes.
type Table[key_t comparable, value_t any] struct {
	m sync.Map // k -> timedV
}

// Store saves the given k/v and sets them to expire after the given time.
// If a value was previously associated to this key, it will be overwritten and its timer reset.
// cleanup functions will be called in given order after the key is deleted from the table.
// Each is given the key and value associated to the record that expired (these values will be the same for each clean up function).
func (tbl *Table[k, v]) Store(key k, value v, expire time.Duration, cleanup ...func(k, v)) {
	// stop existing timer and delete prior value (if applicable)
	if tmp, found := tbl.m.LoadAndDelete(key); found {
		tVal, ok := tmp.(timedV[v])
		if !ok {
			panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
		}
		tVal.exp.Stop()

	}
	// insert the new k/v
	tVal := timedV[v]{
		val: value,
		exp: time.AfterFunc(expire, func() {
			// if the time expires, remove ourself
			tbl.Delete(key)
			// execute additional clean up functions
			for _, f := range cleanup {
				f(key, value)
			}
		})}

	tbl.m.Store(key, tVal)
}

// Load fetches the value associated to the given key if available.
func (tbl *Table[key_t, value_t]) Load(key key_t) (value value_t, found bool) {
	tmp, found := tbl.m.Load(key)
	if !found {
		return value, false
	}
	tVal, ok := tmp.(timedV[value_t])
	if !ok {
		panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
	}

	return tVal.val, true
}

// Delete destroys a key in the map and stops its timer (if found).
// Ineffectual if key is not found.
func (tbl *Table[key_t, value_t]) Delete(key key_t) (found bool) {
	tmp, found := tbl.m.LoadAndDelete(key)
	if !found {
		return false
	}
	tVal, ok := tmp.(timedV[value_t])
	if !ok {
		panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
	}
	tVal.exp.Stop()

	return true
}

// Refresh refreshes the given key (if it exists) with the given duration.
func (tbl *Table[key_t, value_t]) Refresh(key key_t, expire time.Duration) (found bool) {
	tmp, found := tbl.m.Load(key)
	if !found {
		return false
	}
	tVal, ok := tmp.(timedV[value_t])
	if !ok {
		panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
	}
	alreadyExpired := !tVal.exp.Stop()
	if alreadyExpired {
		return false
	}
	tVal.exp.Reset(expire)
	return true
}

// Range is a standard sync.Map range call and follows all the same rules and caveats.
// They are restated here for ease-of-access:
//
/*
Range calls f sequentially for each key and value present in the map. If f returns false, range stops the iteration.

Range does not necessarily correspond to any consistent snapshot of the Map's contents: no key will be visited more than once, but if the value for any key is stored or deleted concurrently (including by f), Range may reflect any mapping for that key from any point during the Range call. Range does not block other methods on the receiver; even f itself may call any method on m.

Range may be O(N) with the number of elements in the map even if f returns false after a constant number of calls.
*/
func (tbl *Table[key_t, value_t]) Range(f func(key_t, value_t) bool) {
	tbl.m.Range(func(key, value any) bool {
		// type assert k and v
		aKey, ok := key.(key_t)
		if !ok {
			panic("failed to cast any key as key_t despite type safe wrapper")
		}

		tmp, found := tbl.m.Load(key)
		if !found {
			return false
		}
		tVal, ok := tmp.(timedV[value_t])
		if !ok {
			panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
		}
		return f(aKey, tVal.val)
	})
}

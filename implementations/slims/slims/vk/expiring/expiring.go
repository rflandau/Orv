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
func (tbl *Table[k, v]) Store(key k, value v, expire time.Duration, cleanup ...func()) {
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
				f()
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

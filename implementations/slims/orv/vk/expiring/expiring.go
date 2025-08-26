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
	exp *time.Timer
}

// A Table is basically a syncmap, but elements prune themselves after their duration elapses.
// Retains the thread-safety of a normal syncmap.
type Table[key_t comparable, value_t any] struct {
	m sync.Map // k -> timedV
}

// Store saves the given k/v and sets them to expire after the given time.
// If a value was previously associated to this key, it will be overwritten and its timer reset.
func (tbl *Table[k, v]) Store(key k, value v, expire time.Duration) {
	// stop existing timer (if applicable)
	if tmp, found := tbl.m.LoadAndDelete(key); found {
		tVal, ok := tmp.(timedV[v])
		if !ok {
			panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
		}
		tVal.exp.Stop()

	}
	// insert the new k/v
	tVal := timedV[v]{value, time.AfterFunc(expire, func() {
		// if the time expires, remove ourself
		tmp, found := tbl.m.LoadAndDelete(key)
		if !found {
			return
		}
		tVal, ok := tmp.(timedV[v])
		if !ok {
			panic(fmt.Sprintf("failed to cast value from syncmap (%v)", tmp))
		}
		tVal.exp.Stop()
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

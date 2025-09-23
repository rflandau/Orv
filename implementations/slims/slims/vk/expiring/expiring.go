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
// NOTE(rlandau): the underlying mutex table does not get automatically pruned.
// Mutexes will be reused for the same key. // TODO add tbl.Prune()
//
// NOTE(rlandau): accessing elements AT their expiration time is, by its very nature, a race.
// If a timer has not expired, then its associated data is guaranteed to not have been pruned. The inverse is not guaranteed.
type Table[key_t comparable, value_t any] struct {
	wholeMu sync.RWMutex // table-wide mutex. Must be held to interact with elementMus.
	// element-wise mutexes that must be held to interact with that element in m.
	elementMus map[key_t]*sync.Mutex
	elements   map[key_t]timedV[value_t]
}

// Store saves the given k/v and sets them to expire after the given time.
// If a value was previously associated to this key, it will be overwritten and its timer reset.
// cleanup functions will be called in given order after the key is deleted from the table.
// Each is given the key and value associated to the record that expired (these values will be the same for each clean up function).
func (tbl *Table[k, v]) Store(key k, value v, expire time.Duration, cleanup ...func(k, v)) {
	tbl.wholeMu.Lock()
	var mu *sync.Mutex
	// attempt to fetch the element-wise mutex
	mu, found := tbl.elementMus[key]
	if !found || mu == nil {
		// new element, create the associated mutex
		mu = &sync.Mutex{}
		tbl.elementMus[key] = mu
	}
	// step the mutex down to element-wise
	tbl.wholeMu.Unlock()
	mu.Lock()
	// stop existing timer and delete prior value (if applicable)
	if val, found := tbl.elements[key]; found {
		val.exp.Stop()
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

	tbl.elements[key] = tVal
}

// Load fetches the value associated to the given key if available.
func (tbl *Table[key_t, value_t]) Load(key key_t) (value value_t, found bool) {
	var mu *sync.Mutex
	// acquire the item's mutex
	{
		tbl.wholeMu.RLock()
		var found bool
		mu, found = tbl.elementMus[key]
		tbl.wholeMu.RUnlock()
		if !found { // if no mutex exists, neither does the item
			return value, false
		}
	}
	mu.Lock()
	defer mu.Unlock()
	tVal, found := tbl.elements[key]
	if !found {
		return value, false
	}
	return tVal.val, true
}

// Delete destroys a key in the map and stops its timer (if found).
// Ineffectual if key is not found.
func (tbl *Table[key_t, value_t]) Delete(key key_t) (found bool) {
	var mu *sync.Mutex
	// acquire the item's mutex
	{
		tbl.wholeMu.RLock()
		var found bool
		mu, found = tbl.elementMus[key]
		tbl.wholeMu.RUnlock()
		if !found { // if no mutex exists, neither does the item
			return false
		}
	}
	// delete the element
	mu.Lock()
	defer mu.Unlock()
	tVal, found := tbl.elements[key]
	if !found {
		return false
	}
	tVal.exp.Stop()
	delete(tbl.elements, key)
	// delete the mutex for the element
	// TODO but probably not in this version
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

// Package expiring introduces tables with the ability to prune their own elements.
package expiring

import (
	"reflect"
	"sync"
	"time"
)

// wrapped value with an expiration timer attached
type timedV[value_t any] struct {
	val value_t
	exp *time.Timer // expiring timer to clear this function out of the table
}

// prune is a helper function that returns the auto-pruner for this record, with its timer started.
// If it fires, the record will be deleted from tbl, followed by sequential execution of cleanup.
func prune[k comparable, v any](tbl *Table[k, v], key k, value v, expire time.Duration, cleanup ...func(k, v)) *time.Timer {
	return time.AfterFunc(expire, func() {
		// if the time expires, remove ourself
		tbl.Delete(key)
		// execute additional clean up functions
		for _, f := range cleanup {
			f(key, value)
		}
	})
}

// A Table is basically a syncmap, but elements prune themselves after their duration elapses.
// The zero value is NOT ready for use; use expiring.New().
//
// NOTE(rlandau): Tables should only be passed by reference due to underlying mutex use.
//
// NOTE(rlandau): accessing elements AT their expiration time is, by its very nature, a race.
// If a timer has not expired, then its associated data is guaranteed to not have been pruned. The inverse is not guaranteed.
type Table[k comparable, v any] struct {
	wholeMu sync.RWMutex // table-wide mutex. Must be held to interact with elementMus.
	// element-wise mutexes that must be held to interact with that element in m.
	elementMus map[k]*sync.Mutex
	elements   map[k]timedV[v]
}

// New returns an initialized Table ready for use.
func New[key_t comparable, value_t any]() *Table[key_t, value_t] {
	return &Table[key_t, value_t]{
		wholeMu:    sync.RWMutex{},
		elementMus: make(map[key_t]*sync.Mutex),
		elements:   make(map[key_t]timedV[value_t]),
	}
}

// Fetches the mutex for element k.
// If create is set:
// 1) acquires the table-wide write lock.
// 2) creates the mutex if it does not exist, guaranteeing the return will be non-nil.
//
// If create is unset:
// 1) acquires a table-wide read lock.
// 2) returns the mutex if found, nil otherwise.
func (tbl *Table[k, v]) getElementMutex(key k, create bool) *sync.Mutex {
	if create {
		tbl.wholeMu.Lock()
		defer tbl.wholeMu.Unlock()
	} else {
		tbl.wholeMu.RLock()
		defer tbl.wholeMu.RUnlock()
	}
	mu, found := tbl.elementMus[key]
	if !found {
		if !create {
			return nil
		}
		// new element, create the associated mutex
		mu = &sync.Mutex{}
		tbl.elementMus[key] = mu
	}
	return mu
}

// Store saves the given k/v and sets it to expire after the duration.
// If a value was previously associated to this key, it will be overwritten and its timer reset.
// cleanup functions will be called in given order after the key is deleted from the table.
// Each is given the key and value associated to the record that expired (these values will be the same for each clean up function).
func (tbl *Table[k, v]) Store(key k, value v, expire time.Duration, cleanup ...func(k, v)) {
	mu := tbl.getElementMutex(key, true)
	if mu == nil {
		panic("getElementMutex returned a nil mutex despite being instructed to create")
	}
	mu.Lock()
	defer mu.Unlock()
	// stop existing timer and delete prior value (if applicable)
	if val, found := tbl.elements[key]; found {
		val.exp.Stop()
	}

	// insert the new k/v
	tVal := timedV[v]{
		val: value,
		exp: prune(tbl, key, value, expire, cleanup...)}

	tbl.elements[key] = tVal
}

// Load fetches the value associated to the given key if available.
func (tbl *Table[key_t, value_t]) Load(key key_t) (value value_t, found bool) {
	var mu *sync.Mutex = tbl.getElementMutex(key, false)
	if mu == nil {
		return value, false
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
	var mu *sync.Mutex = tbl.getElementMutex(key, false)
	if mu == nil {
		return false
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
	tbl.wholeMu.Lock()
	delete(tbl.elementMus, key)
	tbl.wholeMu.Unlock()
	return true
}

// Refresh refreshes the given key (if it exists) with the given duration.
func (tbl *Table[key_t, value_t]) Refresh(key key_t, expire time.Duration) (found bool) {
	var mu *sync.Mutex = tbl.getElementMutex(key, false)
	if mu == nil {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	tVal, found := tbl.elements[key]
	if !found {
		return false
	}
	if alreadyExpired := !tVal.exp.Stop(); alreadyExpired {
		return false
	}
	tVal.exp.Reset(expire)
	return true
}

// CompareAndSwap atomically swaps the old and new values for key iff the value stored in the map is equal to old.
// If key is not found, old will be compared against the zero value of it (mirroring the behavior of sync.Map).
// Otherwise, reflect.DeepEqual is used for comparison.
//
// expire and cleanup are only used if a swap occurs.
//
// Holds a write lock on the whole table.
// TODO test me
func (tbl *Table[k, v]) CompareAndSwap(key k, old, new v, expire time.Duration, cleanup ...func(k, v)) (swapped bool) {
	// helper function that tests if old is the zero value and install key+new iff it is.
	// If !mutexAlreadyExists, a mutex will also be created for the element.
	// Does not acquire any locks.
	newElemCheck := func(mutexAlreadyExists bool) (swapped bool) {
		// this is a new element, so only perform the swap if old is the zero value
		if !reflect.ValueOf(old).IsZero() {
			return false
		}
		if !mutexAlreadyExists {
			// create the mutex and install it
			tbl.elementMus[key] = &sync.Mutex{}
		}
		// install new and set up the pruner
		tbl.elements[key] = timedV[v]{
			val: new,
			exp: prune(tbl, key, new, expire, cleanup...)}
		return true

	}

	// acquire the full table lock, to be safe in-case we need to create the mutex
	tbl.wholeMu.Lock()
	defer tbl.wholeMu.Unlock()
	if _, found := tbl.elementMus[key]; !found {
		return newElemCheck(false)
	}
	// check the value
	tv, found := tbl.elements[key]
	if !found {
		return newElemCheck(true)
	}
	// compare
	if !reflect.DeepEqual(tv.val, old) {
		return false
	}
	// install the new value
	tbl.elements[key] = timedV[v]{
		val: new,
		exp: prune(tbl, key, new, expire, cleanup...),
	}
	return true

}

// Range is similar to sync.Map.Range(), but acquires a full table read lock.
// Thanks to the use a read lock, you may perform a limited subset of operations on the table from within f.
// Specifically, you may call .Refresh(), .Delete(), and .Load().
// Range exits early if f returns false.
// Because a full table-lock is acquired, this range represents a consistent snapshot of the map and no expirations may occur during the range.
// Do note, however, that expirations may still be queued for the moment Range returns.
func (tbl *Table[key_t, value_t]) RangeLocked(f func(key_t, value_t) bool) {
	tbl.wholeMu.RLock()
	defer tbl.wholeMu.RUnlock()
	for k, v := range tbl.elements {
		if !f(k, v.val) {
			return
		}
	}
}

// Range is similar to sync.Map.Range() and locks each value individually
// (you may NOT interact with the element given to f from within f, as it will deadlock).
// Range exits early if f returns false.
// It does not correspond to a consistent snapshot of the map's contents and timers are not halted during operation.
// As this function does not lock the entire table for its full runtime, it has improved throughput (compared to RangeLocked) when multiple threads are operating on it.
// However, it has substantial lock contention as it reacquires the full table lock repeatedly, so outcomes may vary.
//
// If you need a consistent map state, use RangeLocked().
/*func (tbl *Table[key_t, value_t]) Range(f func(key_t, value_t) bool) {
	tbl.wholeMu.RLock()
	defer tbl.wholeMu.RUnlock()
	for k, v := range tbl.elements {
		if !f(k, v.val) {
			return
		}
	}
}*/

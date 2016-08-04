/*
 * Minio Cloud Storage, (C) 2016 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package dsync

import (
	"sync"
	"fmt"
)

type DRWMutex struct {
	r1          DMutex
	r2          DMutex
	r1Locked    bool
	r2Locked    bool
	w           DMutex // held if there are pending writers
}

func NewDRWMutex(name string) (drw *DRWMutex) {
	return &DRWMutex{
		r1: DMutex{Name: name+"-r1"},
		r2: DMutex{Name: name+"-r2"},
		w: DMutex{Name: name+"-w"}}
}

// RLock locks drw for reading.
func (drw *DRWMutex) RLock() {

	// Check if no write is active, block otherwise
	drw.w.Lock()
	drw.w.Unlock()

	// Lock either R1 or R2
	for i := 0; ; i++ {
		if i % 2 == 0 {
			drw.r1Locked = drw.r1.TryLockTimeout()
			if drw.r1Locked {
				return
			}
 		} else {
			drw.r2Locked = drw.r2.TryLockTimeout()
			if drw.r2Locked {
				return
			}
		}
	}

	//if atomic.AddInt32(&rw.readerCount, 1) < 0 {
	//	// A writer is pending, wait for it.
	//	runtime_Semacquire(&rw.readerSem)
	//}
}

// RUnlock undoes a single RLock call;
// it does not affect other simultaneous readers.
// It is a run-time error if rw is not locked for reading
// on entry to RUnlock.
func (drw *DRWMutex) RUnlock() {

	// Unlock either R1 or R2 (which ever was acquired)
	if drw.r1Locked {
		drw.r1.Unlock()
		drw.r1Locked = false
	}

	if drw.r2Locked {
		drw.r2.Unlock()
		drw.r2Locked = false
	}
}

// Lock locks rw for writing.
// If the lock is already locked for reading or writing,
// Lock blocks until the lock is available.
// To ensure that the lock eventually becomes available,
// a blocked Lock call excludes new readers from acquiring
// the lock.
func (drw *DRWMutex) Lock() {

	// First, resolve competition with other writers.
	drw.w.Lock()

	// Acquire all read locks.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		fmt.Println("Waiting for r1.Lock()")
		drw.r1.Lock()
		fmt.Println("Obtained r1.Lock()")
		drw.r1Locked = true
	}()

	go func() {
		defer wg.Done()
		fmt.Println("Waiting for r2.Lock()")
		drw.r2.Lock()
		fmt.Println("Obtained r2.Lock()")
		drw.r2Locked = true
	}()

	wg.Wait()
}

// Unlock unlocks rw for writing.  It is a run-time error if rw is
// not locked for writing on entry to Unlock.
//
// As with Mutexes, a locked RWMutex is not associated with a particular
// goroutine.  One goroutine may RLock (Lock) an RWMutex and then
// arrange for another goroutine to RUnlock (Unlock) it.
func (drw *DRWMutex) Unlock() {

	// Unlock read locks
	drw.r1.Unlock()
	drw.r2.Unlock()
	drw.r1Locked = false
	drw.r2Locked = false

	// Allow other writers to proceed.
	drw.w.Unlock()
}


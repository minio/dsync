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

package main

import (
	"fmt"
	"github.com/minio/dsync"
	"sync"
)

type Locker struct {
	mu sync.Mutex
	// e.g, when a Lock(name) is held, map[string][]bool{"name" : []bool{true}}
	// when one or more RLock() is held, map[string][]bool{"name" : []bool{false, false}}
	nsMap map[string][]bool
}

func (locker *Locker) Lock(args *dsync.LockArgs, reply *bool) error {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	_, ok := locker.nsMap[args.Name]
	if !ok {
		*reply = true
		locker.nsMap[args.Name] = []bool{true}
		return nil
	}
	*reply = false
	return nil
}

func (locker *Locker) Unlock(args *dsync.LockArgs, reply *bool) error {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	_, ok := locker.nsMap[args.Name]
	if !ok {
		return fmt.Errorf("Unlock attempted on an un-locked entity: %s", args.Name)
	}
	*reply = true
	delete(locker.nsMap, args.Name)
	return nil
}

func (locker *Locker) RLock(args *dsync.LockArgs, reply *bool) error {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	locksHeld, ok := locker.nsMap[args.Name]
	if !ok {
		// First read-lock to be held on *name.
		locker.nsMap[args.Name] = []bool{false}
	} else {
		// Add an entry for this read lock.
		if len(locker.nsMap[args.Name]) == 1 && locker.nsMap[args.Name][0] == true {
			*reply = false
			return nil
		}
		locker.nsMap[args.Name] = append(locksHeld, false)
	}
	*reply = true

	return nil
}

func (locker *Locker) RUnlock(args *dsync.LockArgs, reply *bool) error {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	locksHeld, ok := locker.nsMap[args.Name]
	if !ok {
		return fmt.Errorf("RUnlock attempted on an un-locked entity: %s", args.Name)
	}
	if len(locksHeld) > 1 {
		// Remove one of the read locks held.
		locksHeld = locksHeld[1:]
		locker.nsMap[args.Name] = locksHeld
	} else {
		// Delete the map entry since this is the last read lock held
		// on *name.
		delete(locker.nsMap, args.Name)
	}
	*reply = true
	return nil
}

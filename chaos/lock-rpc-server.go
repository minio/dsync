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
	"errors"
	"fmt"
	"github.com/minio/dsync"
	"log"
	"strconv"
	"sync"
	"time"
)

// used when cached timestamp do not match with what client remembers.
var errInvalidTimestamp = errors.New("Timestamps don't match, server may have restarted.")

const WriteLock = -1

type lockServer struct {
	mutex sync.Mutex
						// Map of locks, with negative value indicating (exclusive) write lock
						// and positive values indicating number of read locks
	lockMap   map[string]int64
	timestamp time.Time // Timestamp set at the time of initialization. Resets naturally on minio server restart.
}

func (l *lockServer) verifyArgs(args *dsync.LockArgs) error {
	if !l.timestamp.Equal(args.Timestamp) {
		return errInvalidTimestamp
	}
	return nil
}

func (l *lockServer) Lock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := l.verifyArgs(args); err != nil {
		return err
	}
	if _, *reply = l.lockMap[args.Name]; !*reply {
		l.lockMap[args.Name] = WriteLock // No locks held on the given name, so claim write lock
	}
	*reply = !*reply // Negate *reply to return true when lock is granted or false otherwise
	return nil
}

func (l *lockServer) Unlock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := l.verifyArgs(args); err != nil {
		return err
	}
	var locksHeld int64
	if locksHeld, *reply = l.lockMap[args.Name]; !*reply { // No lock is held on the given name
		return fmt.Errorf("Unlock attempted on an unlocked entity: %s", args.Name)
	}
	if *reply = locksHeld == WriteLock; !*reply { // Unless it is a write lock
		return fmt.Errorf("Unlock attempted on a read locked entity: %s (%d read locks active)", args.Name, locksHeld)
	}
	delete(l.lockMap, args.Name) // Remove the write lock
	return nil
}

const ReadLock = 1

func (l *lockServer) RLock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := l.verifyArgs(args); err != nil {
		return err
	}
	var locksHeld int64
	if locksHeld, *reply = l.lockMap[args.Name]; !*reply {
		l.lockMap[args.Name] = ReadLock // No locks held on the given name, so claim (first) read lock
		*reply = true
	} else {
		if *reply = locksHeld != WriteLock; *reply { // Unless there is a write lock
			l.lockMap[args.Name] = locksHeld + ReadLock // Grant another read lock
		}
	}
	return nil
}

func (l *lockServer) RUnlock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := l.verifyArgs(args); err != nil {
		return err
	}
	var locksHeld int64
	if locksHeld, *reply = l.lockMap[args.Name]; !*reply { // No lock is held on the given name
		return fmt.Errorf("RUnlock attempted on an unlocked entity: %s", args.Name)
	}
	if *reply = locksHeld != WriteLock; !*reply { // A write-lock is held, cannot release a read lock
		return fmt.Errorf("RUnlock attempted on a write locked entity: %s", args.Name)
	}
	if locksHeld > ReadLock {
		l.lockMap[args.Name] = locksHeld - ReadLock // Remove one of the read locks held
	} else {
		delete(l.lockMap, args.Name) // Remove the (last) read lock
	}
	return nil
}

func (l *lockServer) check() {
	var name *string
	l.mutex.Lock()
	if len(l.lockMap) > 0 {
		for n := range l.lockMap {
			name = &n
		}
	}
	l.mutex.Unlock()
	if name != nil {
		log.Println("lock:", *name)

		const portStart = 12345
		const i = 3
		c := newClient(fmt.Sprintf("127.0.0.1:%d", portStart+i), dsync.RpcPath+"-"+strconv.Itoa(portStart+i))

		var locked bool
		if err := c.Call("Dsync.Lock", &dsync.LockArgs{Name: *name}, &locked); err != nil {
			// fix 'connection refused'
			// we failed to connect back to the client, this can either be due to
			// a) server at client still down
			// b) some network error (and server is up normally)
			//
			// We can ignore the error, and we will retry later to get resolve on this lock
			log.Println("  Dsync.Lock failed:", err)
		} else {
			log.Println("  Dsync.Lock result:", locked)
			if locked { // we are able to acquire the lock again

				// remove lock from map
				l.mutex.Lock()
				delete(l.lockMap, *name) // Remove lock
				l.mutex.Unlock()

				var unlocked bool
				// immediately release lock
				if err := c.Call("Dsync.Unlock", &dsync.LockArgs{Name: *name}, &unlocked); err == nil {
					// Unlock delivered, exit out
					return
				} else if err != nil {
					// Unlock not delivered, too bad, do not retry. Instead rely on the
					// maintenance for stale locks at the other end to release the lock
				}
			}
		}
	}
}

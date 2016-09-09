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
	"sync"
	"time"
)

// used when cached timestamp do not match with what client remembers.
var errInvalidTimestamp = errors.New("Timestamps don't match, server may have restarted.")

type lockRequesterInfo struct {
	writer        bool      // Bool whether write or read lock
	node          string    // Network address of client claiming lock
	rpcPath       string    // RPC path of client claiming lock
	timestamp     time.Time // Timestamp set at the time of initialization
	timeLastCheck time.Time // Timestamp for last check of validity of lock
}

func isWriteLock(lri []lockRequesterInfo) bool {
	return len(lri) == 1 && lri[0].writer
}

type lockServer struct {
	mutex sync.Mutex
	// Map of locks, with negative value indicating (exclusive) write lock
	// and positive values indicating number of read locks
	lockMap   map[string][]lockRequesterInfo
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
	_, *reply = l.lockMap[args.Name]
	if !*reply { // No locks held on the given name, so claim write lock
		l.lockMap[args.Name] = []lockRequesterInfo{lockRequesterInfo{writer: true, node: args.Node, rpcPath: args.RpcPath, timestamp: time.Now(), timeLastCheck: time.Now()}}
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
	var lri []lockRequesterInfo
	lri, *reply = l.lockMap[args.Name]
	if !*reply { // No lock is held on the given name
		return fmt.Errorf("Unlock attempted on an unlocked entity: %s", args.Name)
	}
	if *reply = isWriteLock(lri); !*reply { // Unless it is a write lock
		return fmt.Errorf("Unlock attempted on a read locked entity: %s (%d read locks active)", args.Name, len(lri))
	}
	delete(l.lockMap, args.Name) // Remove the write lock
	return nil
}

func (l *lockServer) RLock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := l.verifyArgs(args); err != nil {
		return err
	}
	var lri []lockRequesterInfo
	lri, *reply = l.lockMap[args.Name]
	if !*reply { // No locks held on the given name, so claim (first) read lock
		l.lockMap[args.Name] = []lockRequesterInfo{lockRequesterInfo{writer: false, node: args.Node, rpcPath: args.RpcPath, timestamp: time.Now(), timeLastCheck: time.Now()}}
		*reply = true
	} else {
		if *reply = !isWriteLock(lri); *reply { // Unless there is a write lock
			l.lockMap[args.Name] = append(l.lockMap[args.Name], lockRequesterInfo{writer: false, node: args.Node, rpcPath: args.RpcPath, timestamp: time.Now(), timeLastCheck: time.Now()})
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
	var lri []lockRequesterInfo
	if lri, *reply = l.lockMap[args.Name]; !*reply { // No lock is held on the given name
		return fmt.Errorf("RUnlock attempted on an unlocked entity: %s", args.Name)
	}
	if *reply = !isWriteLock(lri); !*reply { // A write-lock is held, cannot release a read lock
		return fmt.Errorf("RUnlock attempted on a write locked entity: %s", args.Name)
	}
	if len(lri) > 1 {
		// TODO: Make sure we remove the correct one
		// TODO: Make sure we remove the correct one
		// TODO: Make sure we remove the correct one
		lri = lri[1:]
		l.lockMap[args.Name] = lri // Remove one of the read locks held
	} else {
		delete(l.lockMap, args.Name) // Remove the (last) read lock
	}
	return nil
}

type nameLockRequesterInfoPair struct {
	name string
	lri  lockRequesterInfo
}

func getLongLivedLocks(m map[string][]lockRequesterInfo) []nameLockRequesterInfoPair {

	rslt := []nameLockRequesterInfoPair{}

	for name, lriArray := range m {

		for idx, _ := range lriArray {
			// Check whether enough time has gone by since last check
			if time.Since(lriArray[idx].timeLastCheck) >= LockCheckValidityInterval {
				rslt = append(rslt, nameLockRequesterInfoPair{name: name, lri: lriArray[idx]})
				lriArray[idx].timeLastCheck = time.Now()
			}
		}
	}

	return rslt
}

func (l *lockServer) lockMaintenance() {
	l.mutex.Lock()
	nlripLongLived := getLongLivedLocks(l.lockMap)
	l.mutex.Unlock()

	for _, nlrip := range nlripLongLived {

		c := newClient(nlrip.lri.node, nlrip.lri.rpcPath)

		var locked bool
		if err := c.Call("Dsync.Lock", &dsync.LockArgs{Name: nlrip.name, Node: c.Node(), RpcPath: c.RpcPath()}, &locked); err == nil {

			log.Println("  Dsync.Lock result:", locked)
			if locked { // we are able to acquire the lock again

				// immediately release lock
				go func(nlrip nameLockRequesterInfoPair) {
					var unlocked bool
					if err := c.Call("Dsync.Unlock", &dsync.LockArgs{Name: nlrip.name, Node: c.Node(), RpcPath: c.RpcPath()}, &unlocked); err == nil {
						// Unlock delivered, exit out

						c.Close()

						return
					} else if err != nil {
						// Unlock not delivered, too bad, do not retry. Instead rely on the
						// maintenance for stale locks at the other end to release the lock
					}
				}(nlrip)

				// remove lock from map
				l.mutex.Lock()
				// TODO: For read-lock, iterate over array and remove appropriate lock
				delete(l.lockMap, nlrip.name) // Remove lock
				l.mutex.Unlock()
			}
		} else {
			// fix 'connection refused'
			// we failed to connect back to the client, this can either be due to
			// a) server at client still down
			// b) some network error (and server is up normally)
			//
			// We can ignore the error, and we will retry later to get resolve on this lock
			log.Println("  Dsync.Lock failed:", err)
		}
	}
}

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
	"log"
	"sync"
	"time"

	"github.com/minio/dsync/v3"
)

type lockRequesterInfo struct {
	writer        bool      // Bool whether write or read lock
	serverAddr    string    // Network address of client claiming lock
	rpcPath       string    // RPC path of client claiming lock
	uid           string    // Uid to uniquely identify request of client
	timestamp     time.Time // Timestamp set at the time of initialization
	timeLastCheck time.Time // Timestamp for last check of validity of lock
}

func isWriteLock(lri []lockRequesterInfo) bool {
	return len(lri) == 1 && lri[0].writer
}

type lockServer struct {
	mutex     sync.Mutex
	lockMap   map[string][]lockRequesterInfo
	timestamp time.Time // Timestamp set at the time of initialization. Resets naturally on minio server restart.
}

// Lock - rpc handler for (single) write lock operation.
func (l *lockServer) Lock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	_, *reply = l.lockMap[args.Resource]
	if !*reply { // No locks held on the given name, so claim write lock
		l.lockMap[args.Resource] = []lockRequesterInfo{
			{
				writer:        true,
				uid:           args.UID,
				timestamp:     time.Now().UTC(),
				timeLastCheck: time.Now().UTC(),
			},
		}
	}
	*reply = !*reply // Negate *reply to return true when lock is granted or false otherwise
	return nil
}

// Unlock - rpc handler for (single) write unlock operation.
func (l *lockServer) Unlock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var lri []lockRequesterInfo
	if lri, *reply = l.lockMap[args.Resource]; !*reply { // No lock is held on the given name
		return fmt.Errorf("Unlock attempted on an unlocked entity: %s", args.Resource)
	}
	if *reply = isWriteLock(lri); !*reply { // Unless it is a write lock
		return fmt.Errorf("Unlock attempted on a read locked entity: %s (%d read locks active)", args.Resource, len(lri))
	}
	if !l.removeEntry(args.Resource, args.UID, &lri) {
		return fmt.Errorf("Unlock unable to find corresponding lock for uid: %s", args.UID)
	}
	return nil
}

// RLock - rpc handler for read lock operation.
func (l *lockServer) RLock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	lrInfo := lockRequesterInfo{
		writer:        false,
		uid:           args.UID,
		timestamp:     time.Now().UTC(),
		timeLastCheck: time.Now().UTC(),
	}
	if lri, ok := l.lockMap[args.Resource]; ok {
		if *reply = !isWriteLock(lri); *reply { // Unless there is a write lock
			l.lockMap[args.Resource] = append(l.lockMap[args.Resource], lrInfo)
		}
	} else { // No locks held on the given name, so claim (first) read lock
		l.lockMap[args.Resource] = []lockRequesterInfo{lrInfo}
		*reply = true
	}
	return nil
}

// RUnlock - rpc handler for read unlock operation.
func (l *lockServer) RUnlock(args *dsync.LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var lri []lockRequesterInfo
	if lri, *reply = l.lockMap[args.Resource]; !*reply { // No lock is held on the given name
		return fmt.Errorf("RUnlock attempted on an unlocked entity: %s", args.Resource)
	}
	if *reply = !isWriteLock(lri); !*reply { // A write-lock is held, cannot release a read lock
		return fmt.Errorf("RUnlock attempted on a write locked entity: %s", args.Resource)
	}
	if !l.removeEntry(args.Resource, args.UID, &lri) {
		return fmt.Errorf("RUnlock unable to find corresponding read lock for uid: %s", args.UID)
	}
	return nil
}

// removeEntry either, based on the uid of the lock message, removes a single entry from the
// lockRequesterInfo array or the whole array from the map (in case of a write lock or last read lock)
func (l *lockServer) removeEntry(name, uid string, lri *[]lockRequesterInfo) bool {
	// Find correct entry to remove based on uid
	for index, entry := range *lri {
		if entry.uid == uid {
			if len(*lri) == 1 {
				delete(l.lockMap, name) // Remove the (last) lock
			} else {
				// Remove the appropriate read lock
				*lri = append((*lri)[:index], (*lri)[index+1:]...)
				l.lockMap[name] = *lri
			}
			return true
		}
	}
	return false
}

// Similar to removeEntry but only removes an entry only if the lock entry exists in map.
func (l *lockServer) removeEntryIfExists(nlrip nameLockRequesterInfoPair) {
	// Check if entry is still in map (could have been removed altogether by 'concurrent' (R)Unlock of last entry)
	if lri, ok := l.lockMap[nlrip.name]; ok {
		if !l.removeEntry(nlrip.name, nlrip.lri.uid, &lri) {
			// Remove failed, in case it is a:
			if nlrip.lri.writer {
				// Writer: this should never happen as the whole (mapped) entry should have been deleted
				log.Println("Lock maintenance failed to remove entry for write lock (should never happen)", nlrip.name, nlrip.lri.uid, lri)
			} // Reader: this can happen if multiple read locks were active and
			// the one we are looking for has been released concurrently (so it is fine)
		} // Remove went okay, all is fine
	}
}

type nameLockRequesterInfoPair struct {
	name string
	lri  lockRequesterInfo
}

// getLongLivedLocks returns locks that are older than a certain time and
// have not been 'checked' for validity too soon enough
func getLongLivedLocks(m map[string][]lockRequesterInfo, interval time.Duration) []nameLockRequesterInfoPair {

	rslt := []nameLockRequesterInfoPair{}

	for name, lriArray := range m {

		for idx := range lriArray {
			// Check whether enough time has gone by since last check
			if time.Since(lriArray[idx].timeLastCheck) >= interval {
				rslt = append(rslt, nameLockRequesterInfoPair{name: name, lri: lriArray[idx]})
				lriArray[idx].timeLastCheck = time.Now()
			}
		}
	}

	return rslt
}

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
	"math"
	"math/rand"
	"net/rpc"
	"sync"
	"time"
)

const DRWMutexAcquireTimeout = 25 * time.Millisecond

// A DRWMutex is a distributed mutual exclusion lock.
type DRWMutex struct {
	Name  string
	locks []bool     // Array of nodes that granted a lock
	uids  []string   // Array of uids for verification of sending correct release messages
	m     sync.Mutex // Mutex to prevent multiple simultaneous locks from this node
}

type Granted struct {
	index  int
	locked bool
	uid    string
}

func NewDRWMutex(name string) *DRWMutex {
	return &DRWMutex{
		Name:  name,
		locks: make([]bool, dnodeCount),
		uids:  make([]string, dnodeCount),
	}
}

// Connect to respective lock server nodes on the first Lock() call.
func connectLazy() {
	if clnts == nil {
		panic("rpc client connections weren't initialized.")
	}
	for i := range clnts {
		if clnts[i].rpc != nil {
			continue
		}

		// Pass in unique path (as required by server.HandleHTTP().
		// Ignore failure to connect, the lock server node may join the
		// cluster later.
		clnt, err := rpc.DialHTTPPath("tcp", nodes[i], rpcPaths[i])
		if err != nil {
			clnts[i].SetRPC(nil)
			continue
		}
		clnts[i].SetRPC(clnt)
	}
}

// RLock holds a read lock on dm.
//
// If the lock is already in use, the calling goroutine
// blocks until the mutex is available.
func (dm *DRWMutex) RLock() {
	// Shield RLock() with local mutex in order to prevent more than
	// one broadcast going out at the same time from this node
	dm.m.Lock()
	defer dm.m.Unlock()

	runs, backOff := 1, 1

	for {
		connectLazy()

		// create temp arrays on stack
		locks := make([]bool, dnodeCount)
		ids := make([]string, dnodeCount)

		// try to acquire the lock
		isReadLock := true
		success := lock(clnts, &locks, &ids, dm.Name, isReadLock)
		if success {
			// if success, copy array to object
			copy(dm.locks, locks[:])
			copy(dm.uids, ids[:])
			return
		}

		// We timed out on the previous lock, incrementally wait for a longer back-off time,
		// and try again afterwards
		time.Sleep(time.Duration(backOff) * time.Millisecond)

		backOff += int(rand.Float64() * math.Pow(2, float64(runs)))
		if backOff > 1024 {
			backOff = backOff % 64

			runs = 1 // reset runs
		} else if runs < 10 {
			runs++
		}
	}
}

// Lock locks dm.
//
// If the lock is already in use, the calling goroutine
// blocks until the mutex is available.
func (dm *DRWMutex) Lock() {

	// Shield Lock() with local mutex in order to prevent more than
	// one broadcast going out at the same time from this node
	dm.m.Lock()
	defer dm.m.Unlock()

	runs, backOff := 1, 1

	for {
		connectLazy()

		// create temp arrays on stack
		locks := make([]bool, dnodeCount)
		ids := make([]string, dnodeCount)

		// try to acquire the lock
		isReadLock := false
		success := lock(clnts, &locks, &ids, dm.Name, isReadLock)
		if success {
			// if success, copy array to object
			copy(dm.locks, locks[:])
			copy(dm.uids, ids[:])
			return
		}

		// We timed out on the previous lock, incrementally wait for a longer back-off time,
		// and try again afterwards
		time.Sleep(time.Duration(backOff) * time.Millisecond)

		backOff += int(rand.Float64() * math.Pow(2, float64(runs)))
		if backOff > 1024 {
			backOff = backOff % 64

			runs = 1 // reset runs
		} else if runs < 10 {
			runs++
		}
	}
}

// lock tries to acquire the distributed lock, returning true or false
//
func lock(clnts []*RPCClient, locks *[]bool, uids *[]string, lockName string, isReadLock bool) bool {

	// Create buffered channel of quorum size
	ch := make(chan Granted, dquorum)

	for index, c := range clnts {

		// broadcast lock request to all nodes
		go func(index int, isReadLock bool, c *RPCClient) {
			// All client methods issuing RPCs are thread-safe and goroutine-safe,
			// i.e. it is safe to call them from multiple concurrently running go routines.
			var status bool
			var err error
			if isReadLock {
				err = c.Call("Dsync.RLock", lockName, &status)
			} else {
				err = c.Call("Dsync.Lock", lockName, &status)
			}

			locked, uid := false, ""
			if err == nil {
				locked = status
				// TODO: Get UIOD again
				uid = ""
			} else {
				// If rpc call failed due to connection related errors, reset rpc.Client object
				// to trigger reconnect on subsequent Lock()/Unlock() requests to the same node.
				if IsRPCError(err) {
					clnts[index].SetRPC(nil)
				}
				// silently ignore error, retry later
			}

			ch <- Granted{index: index, locked: locked, uid: uid}

		}(index, isReadLock, c)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	quorum := false

	go func(isReadLock bool) {

		// Wait until we have received (minimally) quorum number of responses or timeout
		i := 0
		done := false
		timeout := time.After(DRWMutexAcquireTimeout)

		for ; i < dnodeCount; i++ {

			select {
			case grant := <-ch:
				if grant.locked {
					// Mark that this node has acquired the lock
					(*locks)[grant.index] = true
					(*uids)[grant.index] = grant.uid
				} else {
					done = true
					//fmt.Println("one lock failed before quorum -- release locks acquired")
					releaseAll(clnts, locks, uids, lockName, isReadLock)
				}

			case <-timeout:
				done = true
				// timeout happened, maybe one of the nodes is slow, count
				// number of locks to check whether we have quorum or not
				if !quorumMet(locks) {
					//fmt.Println("timed out -- release locks acquired")
					releaseAll(clnts, locks, uids, lockName, isReadLock)
				}
			}

			if done {
				break
			}
		}

		// Count locks in order to determine whterh we have quorum or not
		quorum = quorumMet(locks)

		// Signal that we have the quorum
		wg.Done()

		// Wait for the other responses and immediately release the locks
		// (do not add them to the locks array because the DRWMutex could
		//  already has been unlocked again by the original calling thread)
		for ; i < dnodeCount; i++ {
			grantToBeReleased := <-ch
			if grantToBeReleased.locked {
				// release lock
				sendRelease(clnts[grantToBeReleased.index], lockName, grantToBeReleased.uid, isReadLock)
			}
		}
	}(isReadLock)

	wg.Wait()

	return quorum
}

// quorumMet determines whether we have acquired n/2+1 underlying locks or not
func quorumMet(locks *[]bool) bool {

	count := 0
	for _, locked := range *locks {
		if locked {
			count++
		}
	}

	return count >= dquorum
}

// releaseAll releases all locks that are marked as locked
func releaseAll(clnts []*RPCClient, locks *[]bool, ids *[]string, lockName string, isReadLock bool) {

	for lock := 0; lock < dnodeCount; lock++ {
		if (*locks)[lock] {
			sendRelease(clnts[lock], lockName, (*ids)[lock], isReadLock)
			(*locks)[lock] = false
			(*ids)[lock] = ""
		}
	}

}

// hasLock returns whether or not a node participated in granting the lock
func (dm *DRWMutex) hasLock(node string) bool {

	for index, n := range nodes {
		if n == node {
			return dm.locks[index]
		}
	}

	return false
}

// locked returns whether or not we have met the quorum
func (dm *DRWMutex) locked() bool {

	locks := make([]bool, dnodeCount)
	copy(locks[:], dm.locks[:])

	return quorumMet(&locks)
}

// RUnlock releases a read lock held on dm.
//
// It is a run-time error if dm is not locked on entry to RUnlock.
func (dm *DRWMutex) RUnlock() {
	// Verify that we have the lock or panic otherwise (similar to sync.mutex)
	if !dm.locked() {
		panic("dsync: unlock of unlocked distributed mutex")
	}

	// We don't need to wait until we have released all the locks (or the quorum)
	// (a subsequent lock will retry automatically in case it would fail to get
	//  quorum)
	for index, c := range clnts {

		if dm.locks[index] {
			// broadcast lock release to all nodes the granted the lock
			isReadLock := true
			sendRelease(c, dm.Name, dm.uids[index], isReadLock)

			dm.locks[index] = false
		}
	}
}

// Unlock unlocks dm.
//
// It is a run-time error if dm is not locked on entry to Unlock.
func (dm *DRWMutex) Unlock() {

	// Verify that we have the lock or panic otherwise (similar to sync.mutex)
	if !dm.locked() {
		panic("dsync: unlock of unlocked distributed mutex")
	}

	// We don't need to wait until we have released all the locks (or the quorum)
	// (a subsequent lock will retry automatically in case it would fail to get
	//  quorum)
	for index, c := range clnts {

		if dm.locks[index] {
			// broadcast lock release to all nodes the granted the lock
			isReadLock := false
			sendRelease(c, dm.Name, dm.uids[index], isReadLock)

			dm.locks[index] = false
		}
	}
}

// sendRelease sends a release message to a node that previously granted a lock
func sendRelease(c *RPCClient, name, uid string, isReadLock bool) {

	backOffArray := []time.Duration{30 * time.Second, 1 * time.Minute, 3 * time.Minute, 10 * time.Minute, 30 * time.Minute, 1 * time.Hour }

	go func(c *RPCClient, name, uid string) {

		for _, backOff := range backOffArray {

			// Make sure we are connected
			connectLazy()

			// All client methods issuing RPCs are thread-safe and goroutine-safe,
			// i.e. it is safe to call them from multiple concurrently running goroutines.
			var status bool
			var err error
			// TODO: Send UID to server
			if isReadLock {
				if err = c.Call("Dsync.RUnlock", name, &status); err == nil {
					// RUnlock delivered, exit out
					return
				}
			} else {
				if err = c.Call("Dsync.Unlock", name, &status); err == nil {
					// Unlock delivered, exit out
					return
				}
			}

			// If rpc call failed due to connection related errors, reset rpc.Client object
			// to trigger reconnect on subsequent Lock()/Unlock() requests to the same node.
			c.SetRPC(nil)

			// wait
			time.Sleep(backOff)
		}
	}(c, name, uid)
}

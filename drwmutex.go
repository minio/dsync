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
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"
)

// Indicator if logging is enabled.
var dsyncLog bool

func init() {
	// Check for DSYNC_LOG env variable, if set logging will be enabled for failed RPC operations.
	dsyncLog = os.Getenv("DSYNC_LOG") == "1"
}

// DRWMutexAcquireTimeout - tolerance limit to wait for lock acquisition before.
const DRWMutexAcquireTimeout = 25 * time.Millisecond // 25ms.

// A DRWMutex is a distributed mutual exclusion lock.
type DRWMutex struct {
	Name         string
	writeLocks   []bool     // Array of nodes that granted a write lock
	readersLocks [][]bool   // Array of array of nodes that granted reader locks
	m            sync.Mutex // Mutex to prevent multiple simultaneous locks from this node
}

type Granted struct {
	index  int
	locked bool
}

type LockArgs struct {
	Token     string
	Timestamp time.Time
	Name      string
}

func (l *LockArgs) SetToken(token string) {
	l.Token = token
}

func (l *LockArgs) SetTimestamp(tstamp time.Time) {
	l.Timestamp = tstamp
}

func NewDRWMutex(name string) *DRWMutex {
	return &DRWMutex{
		Name:       name,
		writeLocks: make([]bool, dnodeCount),
	}
}

// Lock holds a write lock on dm.
//
// If the lock is already in use, the calling go routine
// blocks until the mutex is available.
func (dm *DRWMutex) Lock() {

	isReadLock := false
	dm.lockBlocking(isReadLock)
}

// RLock holds a read lock on dm.
//
// If one or more read lock are already in use, it will grant another lock.
// Otherwise the calling go routine blocks until the mutex is available.
func (dm *DRWMutex) RLock() {

	isReadLock := true
	dm.lockBlocking(isReadLock)
}

// lockBlocking will acquire either a read or a write lock
//
// The call will block until the lock is granted using a built-in
// timing randomized back-off algorithm to try again until successful
func (dm *DRWMutex) lockBlocking(isReadLock bool) {

	runs, backOff := 1, 1

	for {
		// create temp array on stack
		locks := make([]bool, dnodeCount)

		// try to acquire the lock
		success := lock(clnts, &locks, dm.Name, isReadLock)
		if success {
			// if success, copy array to object
			if isReadLock {
				dm.m.Lock()
				defer dm.m.Unlock()
				// append new array of bools at the end
				dm.readersLocks = append(dm.readersLocks, make([]bool, dnodeCount))
				// and copy stack array into last spot
				copy(dm.readersLocks[len(dm.readersLocks)-1], locks[:])
			} else {
				copy(dm.writeLocks, locks[:])
			}

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
func lock(clnts []RPC, locks *[]bool, lockName string, isReadLock bool) bool {

	// Create buffered channel of quorum size
	ch := make(chan Granted, dnodeCount)

	for index, c := range clnts {

		// broadcast lock request to all nodes
		go func(index int, isReadLock bool, c RPC) {
			// All client methods issuing RPCs are thread-safe and goroutine-safe,
			// i.e. it is safe to call them from multiple concurrently running go routines.
			var locked bool
			if isReadLock {
				if err := c.Call("Dsync.RLock", &LockArgs{Name: lockName}, &locked); err != nil {
					if dsyncLog {
						log.Println("Unable to call Dsync.RLock", err)
					}
				}
			} else {
				if err := c.Call("Dsync.Lock", &LockArgs{Name: lockName}, &locked); err != nil {
					if dsyncLog {
						log.Println("Unable to call Dsync.Lock", err)
					}
				}
			}

			ch <- Granted{index: index, locked: locked}

		}(index, isReadLock, c)
	}

	quorum := false

	var wg sync.WaitGroup
	wg.Add(1)
	go func(isReadLock bool) {

		// Wait until we have either a) received all lock responses, b) received too many 'non-'locks for quorum to be or c) time out
		i, locksFailed := 0, 0
		done := false
		timeout := time.After(DRWMutexAcquireTimeout)

		for ; i < dnodeCount; i++ { // Loop until we acquired all locks

			select {
			case grant := <-ch:
				if grant.locked {
					// Mark that this node has acquired the lock
					(*locks)[grant.index] = true
				} else {
					locksFailed++
					if locksFailed > dnodeCount-dquorum {
						// We know that we are not going to get the lock anymore, so exit out
						// and release any locks that did get acquired
						done = true
						releaseAll(clnts, locks, lockName, isReadLock)
					}
				}

			case <-timeout:
				done = true
				// timeout happened, maybe one of the nodes is slow, count
				// number of locks to check whether we have quorum or not
				if !quorumMet(locks) {
					releaseAll(clnts, locks, lockName, isReadLock)
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
				sendRelease(clnts[grantToBeReleased.index], lockName, isReadLock)
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
func releaseAll(clnts []RPC, locks *[]bool, lockName string, isReadLock bool) {
	for lock := 0; lock < dnodeCount; lock++ {
		if (*locks)[lock] {
			sendRelease(clnts[lock], lockName, isReadLock)
			(*locks)[lock] = false
		}
	}
}

// Unlock unlocks the write lock.
//
// It is a run-time error if dm is not locked on entry to Unlock.
func (dm *DRWMutex) Unlock() {

	// Check if minimally a single bool is set in the writeLocks array
	lockFound := false
	for _, b := range dm.writeLocks {
		if b {
			lockFound = true
			break
		}
	}
	if !lockFound {
		panic("Trying to Unlock() while no Lock() is active")
	}

	isReadLock := false
	unlock(&dm.writeLocks, dm.Name, isReadLock)
}

// RUnlock releases a read lock held on dm.
//
// It is a run-time error if dm is not locked on entry to RUnlock.
func (dm *DRWMutex) RUnlock() {

	// create temp array on stack
	locks := make([]bool, dnodeCount)

	{
		dm.m.Lock()
		defer dm.m.Unlock()
		if len(dm.readersLocks) == 0 {
			panic("Trying to RUnlock() while no RLock() is active")
		}
		// Copy out first element to release it first (FIFO)
		copy(locks, dm.readersLocks[0][:])
		// Drop first element from array
		dm.readersLocks = dm.readersLocks[1:]
	}

	isReadLock := true
	unlock(&locks, dm.Name, isReadLock)
}

func unlock(locks *[]bool, name string, isReadLock bool) {

	// We don't panic like sync.Mutex, when an unlock is issued on an
	// un-locked lock, since the lock rpc server may have restarted and
	// "forgotten" about the lock.

	// We don't need to synchronously wait until we have released all the locks (or the quorum)
	// (a subsequent lock will retry automatically in case it would fail to get quorum)

	for index, c := range clnts {

		if (*locks)[index] {
			// broadcast lock release to all nodes the granted the lock
			sendRelease(c, name, isReadLock)

			(*locks)[index] = false
		}
	}
}

// sendRelease sends a release message to a node that previously granted a lock
func sendRelease(c RPC, name string, isReadLock bool) {

	backOffArray := []time.Duration{
		30 * time.Second, // 30secs.
		1 * time.Minute,  // 1min.
		3 * time.Minute,  // 3min.
		10 * time.Minute, // 10min.
		30 * time.Minute, // 30min.
		1 * time.Hour,    // 1hr.
	}

	go func(c RPC, name string) {

		for _, backOff := range backOffArray {

			// All client methods issuing RPCs are thread-safe and goroutine-safe,
			// i.e. it is safe to call them from multiple concurrently running goroutines.
			var unlocked bool

			if isReadLock {
				if err := c.Call("Dsync.RUnlock", &LockArgs{Name: name}, &unlocked); err == nil {
					// RUnlock delivered, exit out
					return
				} else if err != nil {
					if dsyncLog {
						log.Println("Unable to call Dsync.RUnlock", err)
					}
					if nErr, ok := err.(net.Error); ok && nErr.Timeout() {
						// RUnlock possibly failed with server timestamp mismatch, server may have restarted.
						return
					}
				}
			} else {
				if err := c.Call("Dsync.Unlock", &LockArgs{Name: name}, &unlocked); err == nil {
					// Unlock delivered, exit out
					return
				} else if err != nil {
					if dsyncLog {
						log.Println("Unable to call Dsync.Unlock", err)
					}
					if nErr, ok := err.(net.Error); ok && nErr.Timeout() {
						// Unlock possibly failed with server timestamp mismatch, server may have restarted.
						return
					}
				}
			}

			// Wait..
			time.Sleep(backOff)
		}
	}(c, name)
}

// DRLocker returns a sync.Locker interface that implements
// the Lock and Unlock methods by calling drw.RLock and drw.RUnlock.
func (dm *DRWMutex) DRLocker() sync.Locker {
	return (*drlocker)(dm)
}

type drlocker DRWMutex

func (dr *drlocker) Lock()   { (*DRWMutex)(dr).RLock() }
func (dr *drlocker) Unlock() { (*DRWMutex)(dr).RUnlock() }

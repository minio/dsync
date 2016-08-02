package dsync

import (
	"fmt"
	"github.com/valyala/gorpc"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// A DMutex is a distributed mutual exclusion lock.
type DMutex struct {
	Name  string
	locks []bool // Array of nodes that granted a lock
}

type Granted struct {
	index  int
	locked bool
}

// Lock locks dm.
//
// If the lock is already in use, the calling goroutine
// blocks until the mutex is available.
func (dm *DMutex) Lock() {

	if dm.locked() {
		return
	}

	for {
		locks := make([]bool, n)
		success := lock(&locks, dm.Name)
		if success {
			dm.locks = make([]bool, n)
			copy(dm.locks, locks[:])
			return
		}

		// We timed out on the previous lock, wait for random time,
		// and try again afterwards
		time.Sleep(time.Duration(200+(rand.Float32()*800)) * time.Millisecond)
	}
}

// lock tries to acquire the distributed lock, returning true or false
//
func lock(locks *[]bool, lockName string) bool {

	// Create buffered channel of quorum size
	ch := make(chan Granted, n/2+1)

	for index, node := range nodes {

		// broadcast lock request to all nodes
		go func(index int, node string) {

			c := &gorpc.Client{
				Addr: node, // TCP address of the server.
				LogError: func(format string, args ...interface{}) { /* ignore internal error messages */ },
			}
			c.Start()

			// All client methods issuing RPCs are thread-safe and goroutine-safe,
			// i.e. it is safe to call them from multiple concurrently running go routines.
			resp, err := c.Call(fmt.Sprintf("minio/lock/%s", lockName))

			locked := false
			if err == nil {
				if !strings.HasPrefix(resp.(string), "minio/lock/") {
					log.Fatalf("Unexpected response from the server: %+v", resp)
				}
				parts := strings.Split(resp.(string), "/")
				locked = parts[3] == "true"
			} else {
				// silently ignore error, retry later
			}
			c.Stop()

			ch <- Granted{index: index, locked: locked}

		}(index, node)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	quorum := false

	go func() {

		// Wait until we have received (minimally) quorum number of responses or timeout
		i := 0
		done := false
		timeout := time.After(50 * time.Millisecond)

		for ; i < n; i++ {

			select {
			case grant := <-ch:
				if grant.locked {
					// Mark that this node has acquired the lock
					(*locks)[grant.index] = true
				} else {
					done = true
					fmt.Println("one lock failed before quorum -- release locks acquired")
					releaseAll(locks, lockName)
				}

			case <-timeout:
				done = true
				// timeout happened, maybe one of the nodes is slow, count
				// number of locks to check whether we have quorum or not
				if !quorumMet(locks) {
					fmt.Println("timed out -- release locks acquired")
					releaseAll(locks, lockName)
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
		// (do not add them to the locks array because the DMutex could
		//  already has been unlocked again by the original calling thread)
		for ; i < n; i++ {
			grantToBeReleased := <-ch
			if grantToBeReleased.locked {
				// release lock
				go sendRelease(nodes[grantToBeReleased.index], lockName)
			}
		}
	}()

	wg.Wait()

	return quorum
}

// quorumMet determines whether we have acquired n/2+1 underlying locks or not
func quorumMet(locks *[]bool) bool {

	count := 0
	for _, locked := range (*locks) {
		if locked {
			count++
		}
	}

	return count >= n/2+1
}

// releaseAll releases all locks that are marked as locked
func releaseAll(locks *[]bool, lockName string) {

	for lock := 0; lock < n; lock++ {
		if (*locks)[lock] {
			go sendRelease(nodes[lock], lockName)
			(*locks)[lock] = false
		}
	}

}

// hasLock returns whether or not a node participated in granting the lock
func (dm *DMutex) hasLock(node string) bool {

	for index, n := range nodes {
		if n == node {
			return dm.locks[index]
		}
	}

	return false
}

// locked returns whether or not we have met the quorum
func (dm *DMutex) locked() bool {

	locks := make([]bool, n)
	copy(locks[:], dm.locks[:])

	return quorumMet(&locks)
}

// Unlock unlocks dm.
//
// It is a run-time error if dm is not locked on entry to Unlock.
func (dm *DMutex) Unlock() {

	// Verify that we have the lock or panic otherwise (similar to sync.mutex)
	if !dm.locked() {
		panic("dsync: unlock of unlocked distributed mutex")
	}

	// We don't need to wait until we have released all the locks (or the quorum)
	// (a subsequent lock will retry automatically in case it would fail to get
	//  quorum)
	for index, node := range nodes {

		if dm.locks[index] {
			// broadcast lock release to all nodes the granted the lock
			go sendRelease(node, dm.Name)

			dm.locks[index] = false
		}
	}
}

// sendRelease sends a release message to a node that previously granted a lock
func sendRelease(node, name string) {

	c := &gorpc.Client{
		Addr: node, // TCP address of the server.
		LogError: func(format string, args ...interface{}) { /* ignore internal error messages */ },
	}
	c.Start()

	// All client methods issuing RPCs are thread-safe and goroutine-safe,
	// i.e. it is safe to call them from multiple concurrently running goroutines.
	resp, err := c.Call(fmt.Sprintf("minio/unlock/%s", name))
	if err == nil {
		if !strings.HasPrefix(resp.(string), "minio/unlock/") {
			log.Fatalf("Unexpected response from the server: %+v", resp)
		}
	} else {
		// silently ignore error
	}
	c.Stop()
}

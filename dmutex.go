package main

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
	name  string
	state int32
	sema  uint32
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

	rand.Seed(time.Now().UTC().UnixNano())

	for {
		locks := [N]bool{}
		success := lock(locks, dm.name)
		if success {
			dm.locks = make([]bool, N)
			copy(dm.locks[:], locks[:])
			return
		}

		// We timed out on the previous lock, wait for random time,
		// and try again afterwards
		time.Sleep(time.Duration(/*rand.Float32() * */ 1000) * time.Millisecond)
	}
}

// lock locks dm.
//
// If the lock is already in use, the calling goroutine
// blocks until the mutex is available.
func lock(locks [N]bool, lockName string) bool {

	// Create buffered channel of quorum size
	ch := make(chan Granted, N/2+1)

	for index, node := range nodes {

		// broadcast lock request to all nodes
		go func(index int, node string) {

			c := &gorpc.Client{
				// TCP address of the server.
				Addr: node,
			}
			c.Start()

			// All client methods issuing RPCs are thread-safe and goroutine-safe,
			// i.e. it is safe to call them from multiple concurrently running goroutines.
			resp, err := c.Call(fmt.Sprintf("minio/lock/%s", lockName))
			if err != nil {
				log.Fatalf("Error when sending request to server: %s", err)
			}
			if !strings.HasPrefix(resp.(string), "minio/lock/") {
				log.Fatalf("Unexpected response from the server: %+v", resp)
			}
			fmt.Println(resp)
			parts := strings.Split(resp.(string), "/")
			locked := parts[3] == "true"

			ch <- Granted{index: index, locked: locked}

		}(index, node)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	failed := false

	go func() {

		// Wait until we have received responses from quorum
		i := 0
		for ; i < N/2+1; i++ {

			select {
			case grant := <-ch:
			    if grant.locked {
					fmt.Println("Participating", grant.index)

					// Mark that this node has acquired the lock
					locks[grant.index] = true
				} else {
					failed = true
					fmt.Println("one lock failed before quorum -- release locks acquired")
					for lock := 0; lock < N; lock++ {
						if locks[lock] {
							go sendRelease(nodes[lock], lockName)
							locks[lock] = false
						}
					}
				}

			case <-time.After(1000 * time.Millisecond):
				failed = true
				fmt.Println("timed out -- release locks acquired")
				for lock := 0; lock < N; lock++ {
					if locks[lock] {
						go sendRelease(nodes[lock], lockName)
						locks[lock] = false
					}
				}
			}

			if failed {
				break
			}
		}

		// Signal that we have the quorum (and have acquired the lock)
		wg.Done()

		// Wait for the other responses and immediately release the locks
		// (do not add them to the locks array because the DMutex could
		//  already has been unlocked again by the original calling thread)
		for ; i < N; i++ {
			grantToBeReleased := <-ch
			if grantToBeReleased.locked {
				fmt.Println("To be released", grantToBeReleased.index)

				// release lock
				go sendRelease(nodes[grantToBeReleased.index], lockName)
			}
		}
	}()

	wg.Wait()

	return !failed
}

// HasLock returns whether or not a node participated in granting the lock
func (dm *DMutex) HasLock(node string) bool {

	for index, n := range nodes {
		if n == node {
			return dm.locks[index]
		}
	}

	return false
}

// Unlock unlocks dm.
//
// It is a run-time error if dm is not locked on entry to Unlock.
func (dm *DMutex) Unlock() {

	// TODO: Make sure that we have the lock and panic otherwise
	// panic("dsync: unlock of unlocked distributed mutex")

	// TODO: Decide whether or not we want to wait until we have acknowledges from all nodes?

	// broadcast lock release to all nodes the granted the lock
	for index, node := range nodes {

		if dm.locks[index] {
			go sendRelease(node, dm.name)

			dm.locks[index] = false
		}
	}
}

// sendRelease sends a release message to a node that previously granted a lock
func sendRelease(node, name string) {

	c := &gorpc.Client{
		// TCP address of the server.
		Addr: node,
	}
	c.Start()

	// All client methods issuing RPCs are thread-safe and goroutine-safe,
	// i.e. it is safe to call them from multiple concurrently running goroutines.
	resp, err := c.Call(fmt.Sprintf("minio/unlock/%s", name))
	if err != nil {
		log.Fatalf("Error when sending request to server: %s", err)
	}
	if !strings.HasPrefix(resp.(string), "minio/unlock/") {
		log.Fatalf("Unexpected response from the server: %+v", resp)
	}
}

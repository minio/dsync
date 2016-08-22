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

// GOMAXPROCS=10 go test

package dsync

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

const N = 4           // number of lock servers for tests.
var nodes []string    // list of node IP addrs or hostname with ports.
var rpcPaths []string // list of rpc paths were lock server is serving.

type Locker struct {
	mu sync.Mutex
	// e.g, when a Lock(name) is held, map[string][]bool{"name" : []bool{true}}
	// when one or more RLock() is held, map[string][]bool{"name" : []bool{false, false}}
	nsMap map[string][]bool
}

func (locker *Locker) Lock(args *LockArgs, reply *bool) error {
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

func (locker *Locker) Unlock(args *LockArgs, reply *bool) error {
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

func (locker *Locker) RLock(args *LockArgs, reply *bool) error {
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

func (locker *Locker) RUnlock(args *LockArgs, reply *bool) error {
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

func startRPCServers(nodes []string) {

	for i := range nodes {
		server := rpc.NewServer()
		server.RegisterName("Dsync", &Locker{
			mu:    sync.Mutex{},
			nsMap: make(map[string][]bool),
		})
		// For some reason the registration paths need to be different (even for different server objs)
		server.HandleHTTP(rpcPaths[i], fmt.Sprintf("%s-debug", rpcPaths[i]))
		l, e := net.Listen("tcp", ":"+strconv.Itoa(i+12345))
		if e != nil {
			log.Fatal("listen error:", e)
		}
		go http.Serve(l, nil)
	}

	// Let servers start
	time.Sleep(10 * time.Millisecond)
}

// TestMain initializes the testing framework
func TestMain(m *testing.M) {

	rand.Seed(time.Now().UTC().UnixNano())

	nodes = make([]string, 4)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("127.0.0.1:%d", i+12345)
	}
	for i := range nodes {
		rpcPaths = append(rpcPaths, RpcPath+"-"+strconv.Itoa(i))
	}

	// Initialize net/rpc clients for dsync.
	var clnts []RPC
	for i := 0; i < len(nodes); i++ {
		clnts = append(clnts, newClient(nodes[i], rpcPaths[i]))
	}

	if err := SetNodesWithClients(clnts); err != nil {
		log.Fatalf("set nodes failed with %v", err)
	}
	startRPCServers(nodes)

	os.Exit(m.Run())
}

func TestSimpleLock(t *testing.T) {

	dm := NewDRWMutex("test")

	dm.Lock()

	fmt.Println("Lock acquired, waiting...")
	time.Sleep(2500 * time.Millisecond)

	dm.Unlock()
}

func TestSimpleLockUnlockMultipleTimes(t *testing.T) {

	dm := NewDRWMutex("test")

	dm.Lock()
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock()
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock()
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock()
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock()
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()
}

// Test two locks for same resource, one succeeds, one fails (after timeout)
func TestTwoSimultaneousLocksForSameResource(t *testing.T) {

	dm1st := NewDRWMutex("aap")
	dm2nd := NewDRWMutex("aap")

	dm1st.Lock()

	// Release lock after 10 seconds
	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("Unlocking dm1")

		dm1st.Unlock()
	}()

	dm2nd.Lock()

	fmt.Printf("2nd lock obtained after 1st lock is released\n")
	time.Sleep(2500 * time.Millisecond)

	dm2nd.Unlock()
}

// Test three locks for same resource, one succeeds, one fails (after timeout)
func TestThreeSimultaneousLocksForSameResource(t *testing.T) {

	dm1st := NewDRWMutex("aap")
	dm2nd := NewDRWMutex("aap")
	dm3rd := NewDRWMutex("aap")

	dm1st.Lock()

	// Release lock after 10 seconds
	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("Unlocking dm1")

		dm1st.Unlock()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		dm2nd.Lock()

		// Release lock after 10 seconds
		go func() {
			time.Sleep(2500 * time.Millisecond)
			fmt.Println("Unlocking dm2")

			dm2nd.Unlock()
		}()

		dm3rd.Lock()

		fmt.Printf("3rd lock obtained after 1st & 2nd locks are released\n")
		time.Sleep(2500 * time.Millisecond)

		dm3rd.Unlock()
	}()

	go func() {
		defer wg.Done()

		dm3rd.Lock()

		// Release lock after 10 seconds
		go func() {
			time.Sleep(2500 * time.Millisecond)
			fmt.Println("Unlocking dm3")

			dm3rd.Unlock()
		}()

		dm2nd.Lock()

		fmt.Printf("2nd lock obtained after 1st & 3rd locks are released\n")
		time.Sleep(2500 * time.Millisecond)

		dm2nd.Unlock()
	}()

	wg.Wait()
}

// Test two locks for different resources, both succeed
func TestTwoSimultaneousLocksForDifferentResources(t *testing.T) {

	dm1 := NewDRWMutex("aap")
	dm2 := NewDRWMutex("noot")

	dm1.Lock()
	dm2.Lock()

	fmt.Println("Both locks acquired, waiting...")
	time.Sleep(2500 * time.Millisecond)

	dm1.Unlock()
	dm2.Unlock()

	time.Sleep(10 * time.Millisecond)
}

// Borrowed from mutex_test.go
func HammerMutex(m *DRWMutex, loops int, cdone chan bool) {
	for i := 0; i < loops; i++ {
		m.Lock()
		m.Unlock()
	}
	cdone <- true
}

// Borrowed from mutex_test.go
func TestMutex(t *testing.T) {
	m := NewDRWMutex("")
	c := make(chan bool)
	for i := 0; i < 10; i++ {
		go HammerMutex(m, 1000, c)
	}
	for i := 0; i < 10; i++ {
		<-c
	}
}

func BenchmarkMutexUncontended(b *testing.B) {
	type PaddedMutex struct {
		DRWMutex
		pad [128]uint8
	}
	b.RunParallel(func(pb *testing.PB) {
		var mu PaddedMutex
		mu.locks = make([]bool, N)
		mu.uids = make([]string, N)
		for pb.Next() {
			mu.Lock()
			mu.Unlock()
		}
	})
}

func benchmarkMutex(b *testing.B, slack, work bool) {
	mu := NewDRWMutex("")
	if slack {
		b.SetParallelism(10)
	}
	b.RunParallel(func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			mu.Lock()
			mu.Unlock()
			if work {
				for i := 0; i < 100; i++ {
					foo *= 2
					foo /= 2
				}
			}
		}
		_ = foo
	})
}

func BenchmarkMutex(b *testing.B) {
	benchmarkMutex(b, false, false)
}

func BenchmarkMutexSlack(b *testing.B) {
	benchmarkMutex(b, true, false)
}

func BenchmarkMutexWork(b *testing.B) {
	benchmarkMutex(b, false, true)
}

func BenchmarkMutexWorkSlack(b *testing.B) {
	benchmarkMutex(b, true, true)
}

func BenchmarkMutexNoSpin(b *testing.B) {
	// This benchmark models a situation where spinning in the mutex should be
	// non-profitable and allows to confirm that spinning does not do harm.
	// To achieve this we create excess of goroutines most of which do local work.
	// These goroutines yield during local work, so that switching from
	// a blocked goroutine to other goroutines is profitable.
	// As a matter of fact, this benchmark still triggers some spinning in the mutex.
	m := NewDRWMutex("")
	var acc0, acc1 uint64
	b.SetParallelism(4)
	b.RunParallel(func(pb *testing.PB) {
		c := make(chan bool)
		var data [4 << 10]uint64
		for i := 0; pb.Next(); i++ {
			if i%4 == 0 {
				m.Lock()
				acc0 -= 100
				acc1 += 100
				m.Unlock()
			} else {
				for i := 0; i < len(data); i += 4 {
					data[i]++
				}
				// Elaborate way to say runtime.Gosched
				// that does not put the goroutine onto global runq.
				go func() {
					c <- true
				}()
				<-c
			}
		}
	})
}

func BenchmarkMutexSpin(b *testing.B) {
	// This benchmark models a situation where spinning in the mutex should be
	// profitable. To achieve this we create a goroutine per-proc.
	// These goroutines access considerable amount of local data so that
	// unnecessary rescheduling is penalized by cache misses.
	m := NewDRWMutex("")
	var acc0, acc1 uint64
	b.RunParallel(func(pb *testing.PB) {
		var data [16 << 10]uint64
		for i := 0; pb.Next(); i++ {
			m.Lock()
			acc0 -= 100
			acc1 += 100
			m.Unlock()
			for i := 0; i < len(data); i += 4 {
				data[i]++
			}
		}
	})
}

package dsync

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
	"github.com/vburenin/nsync"
	"github.com/valyala/gorpc"
	"strings"
	"log"
)

func startServer(port int, f func(clientAddr string, request interface{}, m *nsync.NamedMutex) interface{}) {

	m := nsync.NewNamedMutex()

	s := &gorpc.Server{
		// Accept clients on this TCP address.
		Addr: fmt.Sprintf(":%d", port),

		Handler: func(clientAddr string, request interface{}) interface{} {
			// Wrap handler function to pass in state
			return f(clientAddr, request, m)
		},
	}
	if err := s.Serve(); err != nil {
		log.Fatalf("Cannot start rpc server: %s", err)
	}
}

func handler(clientAddr string, request interface{}, m *nsync.NamedMutex) interface{} {

	parts := strings.Split(request.(string), "/")
	if parts[1] == "lock" {
		success := m.TryLockTimeout(parts[2], 1*time.Second)

		return fmt.Sprintf("%s/%v", strings.Join(parts, "/"), success)
	} else if parts[1] == "unlock" {

		m.Unlock(parts[2])

		return request
	} else {
		log.Println("Received unknown cmd", parts[1])
	}

	return request
}

func startServers() []string {

	nodes := make([]string, 8)

	for i := 0; i < len(nodes); i++ {
		nodes[i] = fmt.Sprintf("127.0.0.1:%d", i+12345)

		go startServer(i+12345, handler)
	}

	// Let servers start
	time.Sleep(10 * time.Millisecond)

	return nodes
}

// TestMain initializes the testing framework
func TestMain(m *testing.M) {

	rand.Seed(time.Now().UTC().UnixNano())

	nodes := startServers()
	SetNodes(nodes)

	os.Exit(m.Run())
}

func TestSimpleLock(t *testing.T) {

	dm := DMutex{name: "test"}

	dm.Lock()

	fmt.Println("Lock acquired, waiting...")
	time.Sleep(2500 * time.Millisecond)

	dm.Unlock()
}

func TestSimpleLockingTwice(t *testing.T) {

	dm := DMutex{name: "test"}

	dm.Lock()
	dm.Lock()
	dm.Lock()

	fmt.Println("Lock acquired, waiting...")
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)

	dm.Unlock()
}

func TestUnlockPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("unlock of unlocked distributed mutex did not panic")
		}
	}()

	mu := DMutex{name: "test"}
	mu.Lock()
	mu.Unlock()
	mu.Unlock()
}

func TestSimpleLockUnlockMultipleTimes(t *testing.T) {

	dm := DMutex{name: "test"}

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

	dm1st := DMutex{name: "aap"}
	dm2nd := DMutex{name: "aap"}

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

	dm1st := DMutex{name: "aap"}
	dm2nd := DMutex{name: "aap"}
	dm3rd := DMutex{name: "aap"}

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

	dm1 := DMutex{name: "aap"}
	dm2 := DMutex{name: "noot"}

	dm1.Lock()
	dm2.Lock()

	fmt.Println("Both locks acquired, waiting...")
	time.Sleep(2500 * time.Millisecond)

	dm1.Unlock()
	dm2.Unlock()
}

func TestOneNodeDown(t *testing.T) {

}

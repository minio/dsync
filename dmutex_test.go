package dsync

import (
	"fmt"
	"os"
	"time"
	"testing"
	"math/rand"
)

const N = 8

var nodes [N]string

func startServers() {

	for i := 0; i < N; i++ {
		nodes[i] = fmt.Sprintf("127.0.0.1:%d", i+12345)

		go startServer(i + 12345)
	}

	// Let servers start
	time.Sleep(10 * time.Millisecond)
}

// TestMain initializes the testing framework
func TestMain(m *testing.M) {

	rand.Seed(time.Now().UTC().UnixNano())

	// Start test servers
	startServers()

	os.Exit(m.Run())
}

func TestSimpleLock(t *testing.T) {

	dm := DMutex{name: "test"}

	dm.Lock()

	fmt.Println("Lock acquired, waiting...")
	time.Sleep(2500 * time.Millisecond)

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
}
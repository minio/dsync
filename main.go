package main

import (
	"fmt"
	"time"
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

// Test a single lock (always succeeds)
func testSingleLock() {

	dm := DMutex{}

	fmt.Println("Before locking")
	dm.Lock()
	fmt.Println("After locking")

	for i := 0; i < N; i++ {
		fmt.Println("Node", i, dm.HasLock(nodes[i]))
	}

	fmt.Println("We have the lock, waiting...")
	time.Sleep(2500 * time.Millisecond)

	fmt.Println("Before unlocking")
	dm.Unlock()
	fmt.Println("After unlocking")
}

// Test two locks for same resource, one succeeds, one fails (after timeout)
func testTwoSimultaneousLocksForSameResource() {

	dm1 := DMutex{name: "aap"}
	dm2 := DMutex{name: "aap"}

	dm1.Lock()

	go func() {
		time.Sleep(30 * time.Second)
		fmt.Println("Unlocking dm1")

		dm1.Unlock()
	}()

	dm2.Lock()

	fmt.Printf("\n\nWe have the other lock after first is released!\n")
	time.Sleep(2500 * time.Millisecond)

	dm2.Unlock()

}

// Test two locks for same resource, one succeeds, one fails (after timeout)
func testTwoLocksForSameResourceAfterEachOther() {

}

// Test two locks for different resources, both succeed
func testTwoSimultaneousLocksForDifferentResources() {

	dm1 := DMutex{name: "aap"}
	dm2 := DMutex{name: "noot"}

	dm1.Lock()
	dm2.Lock()

	fmt.Println("We have both locks, waiting...")
	time.Sleep(2500 * time.Millisecond)

	dm1.Unlock()
	dm2.Unlock()

}

func main() {

	startServers()

	testTwoSimultaneousLocksForSameResource()
	//testTwoSimultaneousLocksForDifferentResources()

	for j := 0; j < 25; j++ {
		time.Sleep(10 * time.Millisecond)
	}
}

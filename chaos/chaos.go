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
	"flag"
	"fmt"
	"github.com/minio/dsync"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	portFlag = flag.Int("p", portStart, "Port for server to listen on")
	writeLockFlag = flag.String("w", "", "Name of write lock to acquire")
	readLockFlag = flag.String("r", "", "Name of read lock to acquire")
	servers  []*exec.Cmd
)

const chaosName = "chaos"
const n = 4
const portStart = 12345

// testNotEnoughServersForQuorum verifies that when quorum cannot be achieved that locking will block.
// Once another server comes up and quorum becomes possible, the lock will be granted
func testNotEnoughServersForQuorum(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testNotEnoughServersForQuorum")

	// first kill half the quorum of servers
	for k := len(servers) - 1; k >= n/2; k-- {
		cmd := servers[k]
		servers = servers[0:k]
		killProcess(cmd)
	}

	// launch a new server after some time
	go func() {
		time.Sleep(7 * time.Second)
		log.Println("Launching extra server")
		servers = append(servers, launchTestServers(n/2, 1)...)
	}()

	dm := dsync.NewDRWMutex("test")

	log.Println("Trying to acquire lock but too few servers active...")
	dm.Lock()
	log.Println("Acquired lock")

	time.Sleep(2 * time.Second)

	// kill extra server (quorum not available anymore)
	log.Println("Killing extra server")
	cmd := servers[n/2]
	servers = servers[0 : n/2]
	killProcess(cmd)

	dm.Unlock()
	log.Println("Released lock")

	// launch new server again after some time
	go func() {
		time.Sleep(5 * time.Second)
		log.Println("Launching extra server again")
		servers = append(servers, launchTestServers(n/2, 1)...)
	}()

	log.Println("Trying to acquire lock again but too few servers active...")
	dm.Lock()
	log.Println("Acquired lock again")

	dm.Unlock()
	log.Println("Released lock")

	// spin up servers again
	for k := len(servers); k < n; k++ {
		servers = append(servers, launchTestServers(k, 1)...)
	}

	log.Println("**PASSED** testNotEnoughServersForQuorum")
}

// testServerGoingDown tests that a lock is granted when all servers are up, after too
// many servers die that a new lock will block and once servers are up again, the lock is granted.
func testServerGoingDown(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testServerGoingDown")

	dm := dsync.NewDRWMutex("test")

	dm.Lock()
	log.Println("Acquired lock")

	time.Sleep(100 * time.Millisecond)

	dm.Unlock()
	log.Println("Released lock")

	// kill half the quorum of servers
	for k := len(servers) - 1; k >= n/2; k-- {
		cmd := servers[k]
		servers = servers[0:k]
		killProcess(cmd)
	}
	log.Println("Killed half the servers")

	// spin up servers after some time
	go func() {
		time.Sleep(5 * time.Second)
		for k := len(servers); k < n; k++ {
			servers = append(servers, launchTestServers(k, 1)...)
		}
		log.Println("All servers active again")
	}()

	log.Println("Trying to acquire lock...")
	dm.Lock()
	log.Println("Acquired lock again")

	dm.Unlock()
	log.Println("Released lock")

	log.Println("**PASSED** testServerGoingDown")
}

// testServerDownDuringLock verifies that if a server goes down while a lock is held, and comes back later
// another lock on the same name is not granted too early
func testSingleServerOverQuorumDownDuringLock(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testSingleServerOverQuorumDownDuringLock")

	// make sure that we just have enough quorum
	// kill half the quorum of servers
	for k := len(servers) - 1; k >= n/2+1; k-- {
		cmd := servers[k]
		servers = servers[0:k]
		killProcess(cmd)
	}
	log.Println("Killed just enough servers to keep quorum")

	dm := dsync.NewDRWMutex("test")

	// acquire lock
	dm.Lock()
	log.Println("Acquired lock")

	// kill one server which will lose one active lock
	cmd := servers[n/2]
	servers = servers[0 : n/2]
	killProcess(cmd)
	log.Println("Killed one more server to lose quorum")

	// spin up servers after some time
	go func() {
		time.Sleep(2 * time.Second)
		for k := len(servers); k < n; k++ {
			servers = append(servers, launchTestServers(k, 1)...)
		}
		time.Sleep(100 * time.Millisecond)
		log.Println("All servers active again -- but new lock still blocking")

		time.Sleep(6 * time.Second)

		log.Println("About to unlock first lock -- new lock should be granted")
		dm.Unlock()
	}()

	dm2 := dsync.NewDRWMutex("test")

	// try to acquire same lock -- only granted after first lock released
	log.Println("Trying to acquire new lock on same resource...")
	dm2.Lock()
	log.Println("New lock granted")

	// release lock
	dm2.Unlock()
	log.Println("New lock released")

	log.Println("**PASSED** testSingleServerOverQuorumDownDuringLock")
}

// testMultipleServersOverQuorumDownDuringLockKnownError verifies that if multiple servers go down while a lock is held, and come back later
// another lock on the same name is granted too early
//
// Specific deficiency: more than one lock is granted on the same (exclusive) resource
func testMultipleServersOverQuorumDownDuringLockKnownError(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testMultipleServersOverQuorumDownDuringLockKnownError")

	dm := dsync.NewDRWMutex("test")

	// acquire lock
	dm.Lock()
	log.Println("Acquired lock")

	// kill enough servers to free up enough servers to allow new quorum once restarted
	for k := len(servers) - 1; k >= n-(n/2+1); k-- {
		cmd := servers[k]
		servers = servers[0:k]
		killProcess(cmd)
	}
	log.Printf("Killed enough servers to free up enough servers to allow new quorum once restarted (still %d active)", len(servers))

	// spin up servers after some time
	go func() {
		time.Sleep(2 * time.Second)
		for k := len(servers); k < n; k++ {
			servers = append(servers, launchTestServers(k, 1)...)
		}
		time.Sleep(100 * time.Millisecond)
		log.Println("All servers active again -- new lock already granted")

		time.Sleep(5 * time.Second)

		log.Println("About to unlock first lock -- but new lock already granted")
		dm.Unlock()
	}()

	dm2 := dsync.NewDRWMutex("test")

	// try to acquire same lock -- granted once killed servers are up again
	log.Println("Trying to acquire new lock on same resource...")
	dm2.Lock()
	log.Println("New lock granted (too soon)")

	time.Sleep(6 * time.Second)
	// release lock
	dm2.Unlock()
	log.Println("New lock released")

	log.Println("**PASSED WITH KNOWN ERROR** testMultipleServersOverQuorumDownDuringLockKnownError")
}

// testSingleStaleLock verifies that, despite a single stale lock, a new lock can still be acquired on same resource
func testSingleStaleLock(wg *sync.WaitGroup, beforeMaintenanceKicksIn bool) {

	defer wg.Done()

	log.Println("")
	log.Println(fmt.Sprintf("**STARTING** testSingleStaleLock(beforeMaintenanceKicksIn: %v)", beforeMaintenanceKicksIn))

	time.Sleep(500 * time.Millisecond)

	lockName := fmt.Sprintf("single-stale-locks-%v", time.Now())

	// kill last server and restart with a client that acquires 'test-stale'
	killLastServer()
	servers = append(servers, launchTestServersWithLocks(len(servers), 1, lockName, true)...)

	time.Sleep(500 * time.Millisecond)

	// kill all but (this) server -- so we have one stale lock
	for i := len(servers) - 1; i >= 1; i-- {
		killLastServer()
	}

	time.Sleep(500 * time.Millisecond)

	// restart all servers
	servers = append(servers, launchTestServers(len(servers), n-len(servers))...)

	time.Sleep(500 * time.Millisecond)

	// lock on same resource can be acquired despite single server having a stale lock
	dm := dsync.NewDRWMutex(lockName)

	ch := make(chan struct{})

	// try to acquire lock in separate routine (will not succeed)
	go func() {
		log.Println("Trying to get the lock")
		dm.Lock()
		ch <- struct{}{}
	}()

	timeOut := time.After(2 * time.Second)
	if !beforeMaintenanceKicksIn {
		timeOut = time.After(60 * time.Second)
	}

	select {
	case <-ch:
		if beforeMaintenanceKicksIn {
			log.Fatalln("Acquired lock -- SHOULD NOT HAPPEN")
		} else {
			log.Println("Acquired lock")
		}
		dm.Unlock()
		time.Sleep(250 * time.Millisecond) // Allow messages to get out

	case <-timeOut:
		if beforeMaintenanceKicksIn {
			log.Println("Timed out (expected)")
		} else {
			log.Fatalln("Timed out -- SHOULD NOT HAPPEN")
		}
	}

	log.Println(fmt.Sprintf("**PASSED** testSingleStaleLock(beforeMaintenanceKicksIn: %v)", beforeMaintenanceKicksIn))
}

// testMultipleStaleLocks verifies that
// (before maintenance) multiple stale locks will prevent a new lock from being granted
// ( after maintenance) multiple stale locks not will prevent a new lock from being granted
func testMultipleStaleLocks(wg *sync.WaitGroup, beforeMaintenanceKicksIn bool) {

	defer wg.Done()

	log.Println("")
	log.Println(fmt.Sprintf("**STARTING** testMultipleStaleLocks(beforeMaintenanceKicksIn: %v)", beforeMaintenanceKicksIn))

	time.Sleep(500 * time.Millisecond)

	lockName := fmt.Sprintf("multiple-stale-locks-%v", time.Now())
	// kill last server and restart with a client that acquires 'multiple-stale-lock'
	killLastServer()
	servers = append(servers, launchTestServersWithLocks(len(servers), 1, lockName, true)...)

	time.Sleep(500 * time.Millisecond)

	// kill all but (this) server -- so we have one state lock
	for i := len(servers) - 1; i >= n/2; i-- {
		killLastServer()
	}

	time.Sleep(500 * time.Millisecond)

	// restart all servers
	servers = append(servers, launchTestServers(len(servers), n-len(servers))...)

	time.Sleep(500 * time.Millisecond)

	// lock on same resource can not be acquired due to too many servers having a stale lock
	dm := dsync.NewDRWMutex(lockName)

	ch := make(chan struct{})

	// try to acquire lock in separate routine (will not succeed)
	go func() {
		log.Println("Trying to get the lock")
		dm.Lock()
		ch <- struct{}{}
	}()

	timeOut := time.After(2 * time.Second)
	if !beforeMaintenanceKicksIn {
		timeOut = time.After(60 * time.Second)
	}

	select {
	case <-ch:
		if beforeMaintenanceKicksIn {
			log.Fatalln("Acquired lock -- SHOULD NOT HAPPEN")
		} else {
			log.Println("Acquired lock")
		}
		dm.Unlock()
		time.Sleep(250 * time.Millisecond) // Allow messages to get out

	case <-timeOut:
		if beforeMaintenanceKicksIn {
			log.Println("Timed out (expected)")
		} else {
			log.Fatalln("Timed out -- SHOULD NOT HAPPEN")
		}
	}

	log.Println(fmt.Sprintf("**PASSED** testMultipleStaleLocks(beforeMaintenanceKicksIn: %v)", beforeMaintenanceKicksIn))
}

// testClientThatHasLockCrashes verifies that (after a lock maintenance loop)
// multiple stale locks will not prevent a new lock on same resource
func testClientThatHasLockCrashes(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testClientThatHasLockCrashes")

	time.Sleep(500 * time.Millisecond)

	// kill last server and restart with a client that acquires 'test-stale' lock
	killLastServer()
	servers = append(servers, launchTestServersWithLocks(len(servers), 1, "test-stale", true)...)

	time.Sleep(3 * time.Second)

	// crash the last server (creating stale locks at the other servers)
	killLastServer()

	log.Println("Client that has lock crashes; leaving stale locks at other servers")

	time.Sleep(10 * time.Second)

	// spin up crashed server again
	servers = append(servers, launchTestServers(len(servers), 1)...)
	log.Println("Crashed server restarted")

	dm := dsync.NewDRWMutex("test-stale")

	ch := make(chan struct{})

	// try to acquire lock in separate routine (will not succeed)
	go func() {
		log.Println("Trying to get the lock again")
		dm.Lock()
		ch <- struct{}{}
	}()

	select {
	case <-ch:
		log.Println("Acquired lock again")
		dm.Unlock()
		time.Sleep(1 * time.Second) // Allow messages to get out

	case <-time.After(60 * time.Second):
		log.Fatalln("Timed out -- SHOULD NOT HAPPEN")
	}

	log.Println("**PASSED** testClientThatHasLockCrashes")
}

// Same as testClientThatHasLockCrashes but with two clients having read locks
func testTwoClientsThatHaveReadLocksCrash(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testTwoClientsThatHaveReadLocksCrash")

	time.Sleep(500 * time.Millisecond)

	// kill two servers and restart with a client that acquires a read lock on 'test-stale'
	killLastServer()
	killLastServer()
	servers = append(servers, launchTestServersWithLocks(len(servers), 2, "test-stale", false)...)

	time.Sleep(3 * time.Second)

	// crash last two servers (creating stale locks at the other servers)
	killLastServer()
	killLastServer()

	log.Println("Two clients with read locks crashed; leaving stale locks at other servers")

	time.Sleep(10 * time.Second)

	// spin up crashed servers again
	servers = append(servers, launchTestServers(len(servers), 2)...)
	log.Println("Crashed servers restarted")

	dm := dsync.NewDRWMutex("test-stale")

	ch := make(chan struct{})

	// try to acquire lock in separate routine (will not succeed)
	go func() {
		log.Println("Trying to get the lock again")
		dm.Lock()
		ch <- struct{}{}
	}()

	select {
	case <-ch:
		log.Println("Acquired lock again")
		dm.Unlock()
		time.Sleep(1 * time.Second) // Allow messages to get out

	case <-time.After(60 * time.Second):
		log.Fatalln("Timed out -- SHOULD NOT HAPPEN")
	}

	log.Println("**PASSED** testTwoClientsThatHaveReadLocksCrash")

}

func getSelfNode(rpcClnts []dsync.RPC, port int) int {

	index := -1
	for i, c := range rpcClnts {
		p, _ := strconv.Atoi(strings.Split(c.Node(), ":")[1])
		if port == p {
			if index == -1 {
				index = i
			} else {
				panic("More than one port found")
			}
		}
	}
	return index
}

func main() {

	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	if *portFlag != portStart {

		if *writeLockFlag != "" || *readLockFlag != "" {
			go func() {
				// Initialize net/rpc clients for dsync.
				var clnts []dsync.RPC
				for i := 0; i < n; i++ {
					clnts = append(clnts, newClient(fmt.Sprintf("127.0.0.1:%d", portStart+i), dsync.RpcPath+"-"+strconv.Itoa(portStart+i)))
				}

				if err := dsync.SetNodesWithClients(clnts, getSelfNode(clnts, *portFlag)); err != nil {
					log.Fatalf("set nodes failed with %v", err)
				}

				// Give servers some time to start
				time.Sleep(100 * time.Millisecond)

				if *writeLockFlag != "" {
					lock := dsync.NewDRWMutex(*writeLockFlag)
					lock.Lock()
					log.Println("Acquired write lock:", *writeLockFlag, "(never to be released)")
				}
				if *readLockFlag != "" {
					lock := dsync.NewDRWMutex(*readLockFlag)
					lock.RLock()
					log.Println("Acquired read lock:", *readLockFlag, "(never to be released)")
				}

				// We will hold on to the lock
			}()
		}

		// Does not return, will listen on port
		startRPCServer(*portFlag)
	}

	// Make sure no child processes are still running
	if killStaleProcesses(chaosName) {
		os.Exit(-1)
	}

	// For first client, start server and continue
	go startRPCServer(*portFlag)

	servers = []*exec.Cmd{}

	log.SetPrefix(fmt.Sprintf("[%s] ", chaosName))
	log.SetFlags(log.Lmicroseconds)
	servers = append(servers, &exec.Cmd{}) // Add fake process for first entry
	servers = append(servers, launchTestServers(1, n-1)...)

	// Initialize net/rpc clients for dsync.
	var clnts []dsync.RPC
	for i := 0; i < n; i++ {
		clnts = append(clnts, newClient(fmt.Sprintf("127.0.0.1:%d", portStart+i), dsync.RpcPath+"-"+strconv.Itoa(portStart+i)))
	}

	// This process serves as the first server
	if err := dsync.SetNodesWithClients(clnts, getSelfNode(clnts, *portFlag)); err != nil {
		log.Fatalf("set nodes failed with %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	wg := sync.WaitGroup{}

	wg.Add(1)
	go testNotEnoughServersForQuorum(&wg)
	wg.Wait()

	wg.Add(1)
	go testServerGoingDown(&wg)
	wg.Wait()

	wg.Add(1)
	testSingleServerOverQuorumDownDuringLock(&wg)
	wg.Wait()

	wg.Add(1)
	testMultipleServersOverQuorumDownDuringLockKnownError(&wg)
	wg.Wait()

	wg.Add(1)
	testClientThatHasLockCrashes(&wg)
	wg.Wait()

	wg.Add(1)
	testTwoClientsThatHaveReadLocksCrash(&wg)
	wg.Wait()

	wg.Add(1)
	beforeMaintenanceKicksIn := true
	testSingleStaleLock(&wg, beforeMaintenanceKicksIn)
	wg.Wait()

	wg.Add(1)
	beforeMaintenanceKicksIn = false
	testSingleStaleLock(&wg, beforeMaintenanceKicksIn)
	wg.Wait()

	wg.Add(1)
	beforeMaintenanceKicksIn = true
	testMultipleStaleLocks(&wg, beforeMaintenanceKicksIn)
	wg.Wait()

	wg.Add(1)
	beforeMaintenanceKicksIn = false
	testMultipleStaleLocks(&wg, beforeMaintenanceKicksIn)
	wg.Wait()

	// Kill any launched processes
	killStaleProcesses(chaosName)
}

func killStaleProcesses(name string) bool {

	cmd := exec.Command("pgrep", name)
	cmb, _ := cmd.CombinedOutput()
	procs := strings.Count(string(cmb), "\n")
	if procs > 1 {
		fmt.Println("Found more than one", name, "process. Killing all and exiting")
		cmd = exec.Command("pkill", "-SIGKILL", name)
		cmb, _ = cmd.CombinedOutput()
		return true
	}
	return false
}

func launchTestServers(start, number int) []*exec.Cmd {

	result := []*exec.Cmd{}

	for p := portStart + start; p < portStart+start+number; p++ {
		result = append(result, launchProcess(p, "", false))
	}

	return result
}

func launchTestServersWithLocks(start, number int, name string, writeLock bool) []*exec.Cmd {

	result := []*exec.Cmd{}

	for p := portStart + start; p < portStart+start+number; p++ {
		result = append(result, launchProcess(p, name, writeLock))
	}

	return result
}

func launchProcess(port int, name string, writeLock bool) *exec.Cmd {

	var cmd *exec.Cmd
	if name == "" {
		cmd = exec.Command("./"+chaosName, "-p", fmt.Sprintf("%d", port))
	} else if writeLock {
		cmd = exec.Command("./"+chaosName, "-p", fmt.Sprintf("%d", port), "-w", name)
	} else {
		cmd = exec.Command("./"+chaosName, "-p", fmt.Sprintf("%d", port), "-r", name)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	go func(cmd *exec.Cmd) {
		err := cmd.Start()
		if err != nil {
			log.Fatal(err)
		}
	}(cmd)

	return cmd
}

func killLastServer() {
	cmd := servers[len(servers)-1]
	servers = servers[0 : len(servers)-1]
	killProcess(cmd)
}

func killProcess(cmd *exec.Cmd) {
	if err := cmd.Process.Kill(); err != nil {
		log.Fatal("failed to kill: ", err)
	}
}

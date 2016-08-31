package main

import (
	"flag"
	"fmt"
	"github.com/minio/dsync"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	portFlag = flag.Int("p", 0, "Port for server to listen on")
	rpcPaths []string
	servers  []*exec.Cmd
)

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
	servers = servers[0:n/2]
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
func testSingleStaleLock(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testSingleStaleLock")

	// lock is acquired

	// network connection is lost to single server

	// lock is released

	// client that has lock dies (so unlock retries /w back-off mechanism stop)

	// network connection is repaired to lost server

	// client is restarted

	// lock on same resource can be acquired despite single server having a stale lock

}

// testMultipleStaleLocksKnownError verifies that multiple stale locks will prevent a new lock from being granted
func testMultipleStaleLocksKnownError(wg *sync.WaitGroup) {

	defer wg.Done()

	log.Println("")
	log.Println("**STARTING** testMultipleStaleLocksKnownError")

	// lock is acquired

	// network connections are lost to multiple servers (enough to prevent new quorum)

	// lock is released

	// client that has lock dies (so unlock retries /w back-off mechanism stop)

	// network connection is repaired to lost servers

	// client is restarted

	// lock on same resource will fail (block indefinitely) due to too many multiple stale locks
}

func main() {

	flag.Parse()

	if *portFlag != 0 {
		// Does not return, will serve
		startRPCServer(*portFlag)
	}

	// Make sure no child processes are still running
	if countProcesses("chaos") {
		os.Exit(-1)
	}

	servers = []*exec.Cmd{}

	log.SetPrefix(fmt.Sprintf("[chaos] "))
	log.SetFlags(log.Lmicroseconds)
	servers = append(servers, launchTestServers(0, n)...)

	// Initialize net/rpc clients for dsync.
	var clnts []dsync.RPC
	for i := 0; i < n; i++ {
		clnts = append(clnts, newClient(fmt.Sprintf("127.0.0.1:%d", portStart+i), dsync.RpcPath+"-"+strconv.Itoa(portStart+i)))
	}

	if err := dsync.SetNodesWithClients(clnts); err != nil {
		log.Fatalf("set nodes failed with %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	wg := sync.WaitGroup{}

	//wg.Add(1)
	//go testNotEnoughServersForQuorum(&wg)

	//wg.Add(1)
	//go testServerGoingDown(&wg)

	wg.Add(1)
	testSingleServerOverQuorumDownDuringLock(&wg)

	wg.Add(1)
	testMultipleServersOverQuorumDownDuringLockKnownError(&wg)

	wg.Wait()

	/*	// Start server
		startRPCServer(*portFlag)

		dm := dsync.NewDRWMutex(fmt.Sprintf("chaos-%d", *portFlag))

		timeStart := time.Now()
		timeLast := time.Now()
		durationMax := float64(0.0)

		done := false

		// Catch Ctrl-C and abort gracefully with release of locks
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			for sig := range c {
				fmt.Println("Ctrl-C intercepted", sig)
				done = true
			}
		}()

		var run int
		for run = 1; !done && run < 10000; run++ {
			dm.Lock()

			if run == 1 { // re-initialize timing info to account for initial delay to start all nodes
				timeStart = time.Now()
				timeLast = time.Now()
			}

			duration := time.Since(timeLast)
			if durationMax < duration.Seconds() || run%100 == 0 {
				if durationMax < duration.Seconds() {
					durationMax = duration.Seconds()
				}
				fmt.Println("*****\nMax duration: ", durationMax, "\n*****\nAvg duration: ", time.Since(timeStart).Seconds()/float64(run), "\n*****")
			}
			timeLast = time.Now()
			fmt.Println(*portFlag, "locked", time.Now())

			// time.Sleep(1 * time.Millisecond)

			dm.Unlock()
		}

		fmt.Println("*****\nMax duration: ", durationMax, "\n*****\nAvg duration: ", time.Since(timeStart).Seconds()/float64(run), "\n*****\nLocks/sec: ", 1.0 / (time.Since(timeStart).Seconds()/float64(run)), "\n*****")
	*/
}

func countProcesses(name string) bool {

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
		result = append(result, launchProcess(p))
	}

	return result
}

func launchProcess(port int) *exec.Cmd {

	cmd := exec.Command("./chaos", "-p", fmt.Sprintf("%d", port))
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

func killProcess(cmd *exec.Cmd) {
	if err := cmd.Process.Kill(); err != nil {
		log.Fatal("failed to kill: ", err)
	}
}

/*	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-time.After(20 * time.Second):
		if err := cmd.Process.Kill(); err != nil {
			log.Fatal("failed to kill: ", err)
		}
		log.Println("process killed as timeout reached")
	case err := <-done:
		if err != nil {
			log.Printf("process done with error = %v", err)
		} else {
			log.Print("process done gracefully without error")
		}
	}
*/

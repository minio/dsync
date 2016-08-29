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
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/minio/dsync"
)

type Locker struct {
	mu sync.Mutex
	// e.g, when a Lock(name) is held, map[string][]bool{"name" : []bool{true}}
	// when one or more RLock() is held, map[string][]bool{"name" : []bool{false, false}}
	nsMap map[string][]bool
}

func (locker *Locker) Lock(args *dsync.LockArgs, reply *bool) error {
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

func (locker *Locker) Unlock(args *dsync.LockArgs, reply *bool) error {
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

func (locker *Locker) RLock(args *dsync.LockArgs, reply *bool) error {
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

func (locker *Locker) RUnlock(args *dsync.LockArgs, reply *bool) error {
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

var (
	portFlag = flag.Int("p", 0, "Port for server to listen on")
	rpcPaths []string
)

func startRPCServer(port int) {
	server := rpc.NewServer()
	server.RegisterName("Dsync", &Locker{
		mu:    sync.Mutex{},
		nsMap: make(map[string][]bool),
	})
	// For some reason the registration paths need to be different (even for different server objs)
	server.HandleHTTP(rpcPaths[port-12345], fmt.Sprintf("%s-debug", rpcPaths[port-12345]))
	l, e := net.Listen("tcp", ":"+strconv.Itoa(port))
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// RPCClient is a wrapper type for rpc.Client which provides reconnect on first failure.
type RPCClient struct {
	sync.Mutex
	rpc     *rpc.Client
	node    string
	rpcPath string
}

// newClient constructs a RPCClient object with node and rpcPath initialized.
// It _doesn't_ connect to the remote endpoint. See Call method to see when the
// connect happens.
func newClient(node, rpcPath string) dsync.RPC {
	return &RPCClient{
		node:    node,
		rpcPath: rpcPath,
	}
}

// Close closes the underlying socket file descriptor.
func (rpcClient *RPCClient) Close() error {
	rpcClient.Lock()
	defer rpcClient.Unlock()
	// If rpc client has not connected yet there is nothing to close.
	if rpcClient.rpc == nil {
		return nil
	}
	// Reset rpcClient.rpc to allow for subsequent calls to use a new
	// (socket) connection.
	clnt := rpcClient.rpc
	rpcClient.rpc = nil
	return clnt.Close()
}

// Call makes a RPC call to the remote endpoint using the default codec, namely encoding/gob.
func (rpcClient *RPCClient) Call(serviceMethod string, args dsync.TokenSetter, reply interface{}) error {
	rpcClient.Lock()
	defer rpcClient.Unlock()
	// If the rpc.Client is nil, we attempt to (re)connect with the remote endpoint.
	if rpcClient.rpc == nil {
		clnt, err := rpc.DialHTTPPath("tcp", rpcClient.node, rpcClient.rpcPath)
		if err != nil {
			return err
		}
		fmt.Println("connection estalished")
		rpcClient.rpc = clnt
	}

	// If the RPC fails due to a network-related error, then we reset
	// rpc.Client for a subsequent reconnect.
	err := rpcClient.rpc.Call(serviceMethod, args, reply)
	if IsRPCError(err) {
		rpcClient.rpc = nil
	}
	return err

}

// IsRPCError returns true if the error value is due to a network related
// failure, false otherwise.
func IsRPCError(err error) bool {
	if err == nil {
		return false
	}
	// The following are net/rpc specific errors that indicate that
	// the connection may have been reset. Reset rpcClient.rpc to nil
	// to trigger a reconnect in future.
	if err == rpc.ErrShutdown {
		return true
	}
	return false
}

func main() {

	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	if *portFlag == 0 {
		log.Fatalf("No port number specified")
	}

	nodes := []string{"127.0.0.1:12345", "127.0.0.1:12346", "127.0.0.1:12347", "127.0.0.1:12348", "127.0.0.1:12349", "127.0.0.1:12350", "127.0.0.1:12351", "127.0.0.1:12352" , "127.0.0.1:12353", "127.0.0.1:12354", "127.0.0.1:12355", "127.0.0.1:12356" }//, "127.0.0.1:12357", "127.0.0.1:12358", "127.0.0.1:12359", "127.0.0.1:12360"}
	rpcPaths = make([]string, 0, len(nodes)) // list of rpc paths where lock server is serving.
	for i := range nodes {
		rpcPaths = append(rpcPaths, dsync.RpcPath+"-"+strconv.Itoa(i))
	}

	// Initialize net/rpc clients for dsync.
	var clnts []dsync.RPC
	for i := 0; i < len(nodes); i++ {
		clnts = append(clnts, newClient(nodes[i], rpcPaths[i]))
	}

	if err := dsync.SetNodesWithClients(clnts); err != nil {
		log.Fatalf("set nodes failed with %v", err)
	}

	// Start server
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

		// time.Sleep(1/*0*/ * time.Millisecond)

		dm.Unlock()
	}

	fmt.Println("*****\nMax duration: ", durationMax, "\n*****\nAvg duration: ", time.Since(timeStart).Seconds()/float64(run), "\n*****\nLocks/sec: ", 1.0 / (time.Since(timeStart).Seconds()/float64(run)), "\n*****")

	// Let release messages get out
	time.Sleep(10000 * time.Millisecond)
}

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

	"github.com/fwessels/dsync"
)

const rpcPath = "/dsync"
const debugPath = "/debug"

type Locker struct {
	mu    sync.Mutex
	nsMap map[string]struct{}
}

func (locker *Locker) Lock(args *string, reply *bool) error {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	if _, ok := locker.nsMap[*args]; !ok {
		locker.nsMap[*args] = struct{}{}
		*reply = true
		return nil
	}
	*reply = false
	return nil
}

func (locker *Locker) Unlock(args *string, reply *bool) error {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	if _, ok := locker.nsMap[*args]; !ok {
		return fmt.Errorf("Unlock sent on un-locked entity %s", *args)
	} else {
		delete(locker.nsMap, *args)
	}
	return nil
}

var (
	portFlag = flag.Int("p", 0, "Port for server to listen on")
)

func startRPCServer(port int) {
	server := rpc.NewServer()
	server.RegisterName("Dsync", &Locker{
		mu:    sync.Mutex{},
		nsMap: make(map[string]struct{}),
	})
	server.HandleHTTP(rpcPath, debugPath)
	l, e := net.Listen("tcp", ":"+strconv.Itoa(port))
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

func main() {

	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	if *portFlag == 0 {
		log.Fatalf("No port number specified")
	}

	startRPCServer(*portFlag)
	time.Sleep(2 * time.Second)

	nodes := []string{"127.0.0.1:12345", "127.0.0.1:12346", "127.0.0.1:12347", "127.0.0.1:12348"}
	//, "127.0.0.1:12349", "127.0.0.1:12350", "127.0.0.1:12351", "127.0.0.1:12352"}
	if err := dsync.SetNodesWithPath(nodes, rpcPath); err != nil {
		log.Fatalf("set nodes failed with %v", err)
	}

	dm := dsync.DMutex{Name: "chaos"}

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

	for run := 1; !done; run++ {
		dm.Lock()

		duration := time.Since(timeLast)
		if durationMax < duration.Seconds() || run%100 == 0 {
			if durationMax < duration.Seconds() {
				durationMax = duration.Seconds()
			}
			fmt.Println("*****\nMax duration: ", durationMax, "\n*****\nAvg duration: ", time.Since(timeStart).Seconds()/float64(run), "\n*****")
		}
		timeLast = time.Now()
		fmt.Println(*portFlag, "locked", time.Now())

		time.Sleep(10 * time.Millisecond)

		dm.Unlock()
	}

	// Let release messages get out
	time.Sleep(10 * time.Millisecond)
}

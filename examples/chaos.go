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
	"github.com/fwessels/dsync"
	"github.com/valyala/gorpc"
	"github.com/vburenin/nsync"
	"log"
	"strings"
	"time"
	"math/rand"
	"os"
	"os/signal"
)

func startServer(port int, f func(clientAddr string, request interface{}, m *nsync.NamedMutex, mapUID map[string]uint64) interface{}) {

	m := nsync.NewNamedMutex()
	mapUID := make(map[string]uint64)

	s := &gorpc.Server{
		// Accept clients on this TCP address.
		Addr: fmt.Sprintf(":%d", port),

		Handler: func(clientAddr string, request interface{}) interface{} {
			// Wrap handler function to pass in state
			return f(clientAddr, request, m, mapUID)
		},
	}
	if err := s.Serve(); err != nil {
		log.Fatalf("Cannot start rpc server: %s", err)
	}
}

func handler(clientAddr string, request interface{}, m *nsync.NamedMutex, mapUID map[string]uint64) interface{} {

	parts := strings.Split(request.(string), "/")
	if parts[1] == "lock" {
		success := m.TryLockTimeout(parts[2], 1*time.Second)

		uidstr := ""
		if success {
			uid, ok := mapUID[parts[2]]
			if ok {
				uid += uint64(rand.Int63n(234))
			} else {
				uid = uint64(rand.Int63n(123))
			}
			mapUID[parts[2]] = uid
			uidstr = fmt.Sprintf("%v", uid)
		}
		return fmt.Sprintf("%s/%v", strings.Join(parts, "/"), uidstr)
	} else if parts[1] == "unlock" {

		uid, ok := mapUID[parts[2]]
		if !ok {
			fmt.Println("unlock called on lock that is not locked", parts[2])
		} else if parts[3] != fmt.Sprintf("%v", uid) {
			fmt.Println("UID for release does not match: ", parts[3], fmt.Sprintf("%v", uid))
		}
		m.Unlock(parts[2])

		return request
	} else {
		log.Println("Received unknown cmd", parts[1])
	}

	return request
}

func startServerOnPort(port int) {

	go startServer(port, handler)

	// Let servers start
	time.Sleep(10 * time.Millisecond)
}

var (
	portFlag = flag.Int("p", 0, "Port for server to listen on")
)

func main() {

	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	if *portFlag == 0 {
		log.Fatalf("No port number specified")
	}
	startServerOnPort(*portFlag)

	nodes := []string{"127.0.0.1:12345", "127.0.0.1:12346", "127.0.0.1:12347", "127.0.0.1:12348", "127.0.0.1:12349", "127.0.0.1:12350", "127.0.0.1:12351", "127.0.0.1:12352"}
	dsync.SetNodes(nodes)

	dm := dsync.DMutex{Name: "chaos"}

	timeStart := time.Now()
	timeLast := time.Now()
	durationMax := float64(0.0)

	done := false

	// Catch Ctrl-C and abort gracefully with release of locks
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func(){
		for sig := range c {
			fmt.Println("Ctrl-C intercepted", sig)
			done = true
		}
	}()

	for run := 1; !done ; run++ {
		dm.Lock()

		duration := time.Since(timeLast)
		if durationMax < duration.Seconds() || run % 100 == 0 {
			if durationMax < duration.Seconds() {
				durationMax = duration.Seconds()
			}
			fmt.Println("*****\nMax duration: ", durationMax, "\n*****\nAvg duration: ", time.Since(timeStart).Seconds() / float64(run), "\n*****")
		}
		timeLast = time.Now()
		fmt.Println(*portFlag, "locked", time.Now())

		time.Sleep(10 * time.Millisecond)

		dm.Unlock()
	}

	// Let release messages get out
	time.Sleep(100 * time.Millisecond)
}

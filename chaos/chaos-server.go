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
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// For this test framework we have short lock timings
// Production should use eg following values
//
// const LockMaintenanceLoop       = 1 * time.Minute
// const LockCheckValidityInterval = 2 * time.Minute
// const LockMaintenanceHealthThreshold = 5 * LockMaintenanceLoop
//
const LockMaintenanceLoop = 1 * time.Second
const LockCheckValidityInterval = 5 * time.Second
const LockMaintenanceHealthThreshold = 10 * LockMaintenanceLoop

func startRPCServer(port int) {
	log.SetPrefix(fmt.Sprintf("[%d] ", port))
	log.SetFlags(log.Lmicroseconds)

	server := rpc.NewServer()
	locker := &lockServer{
		mutex:   sync.Mutex{},
		lockMap: make(map[string][]lockRequesterInfo),
		// timestamp: leave uninitialized for testing (set to real timestamp for actual usage)
	}
	lockHeartBeat := uint64(0)

	lockMaintainer := func() {
		// Start with random sleep time, so as to avoid "synchronous checks" between servers
		time.Sleep(time.Duration(rand.Float64() * float64(LockMaintenanceLoop)))
		for {
			time.Sleep(LockMaintenanceLoop)
			locker.lockMaintenance(LockCheckValidityInterval)
			atomic.AddUint64(&lockHeartBeat, 1)
		}
	}
	lockMonitor := func() {
		lastHeartBeat := uint64(0)
		lastHeartBeatStamp := time.Now()
		for {
			time.Sleep(LockMaintenanceLoop)
			currentHeartBeat := atomic.LoadUint64(&lockHeartBeat)
			if currentHeartBeat > lastHeartBeat {
				lastHeartBeat = currentHeartBeat
				lastHeartBeatStamp = time.Now()
			}
			// Check whether not too much time has passed since last 'sign' of life
			// from maintenance loop
			if time.Since(lastHeartBeatStamp) > LockMaintenanceHealthThreshold {
				// reset state to initiate new monitoring
				atomic.StoreUint64(&lockHeartBeat, 0)
				lastHeartBeat = uint64(0)
				lastHeartBeatStamp = time.Now()

				// restart maintainer
				go lockMaintainer()
			}
		}
	}
	go lockMaintainer()
	go lockMonitor()
	server.RegisterName("Dsync", locker)
	// For some reason the registration paths need to be different (even for different server objs)
	rpcPath := rpcPathPrefix + "-" + strconv.Itoa(port)
	server.HandleHTTP(rpcPath, fmt.Sprintf("%s-debug", rpcPath))
	l, e := net.Listen("tcp", ":"+strconv.Itoa(port))
	if e != nil {
		log.Fatal("listen error:", e)
	}
	log.Println("RPC server listening at port", port, "under", rpcPath)
	http.Serve(l, nil)
}

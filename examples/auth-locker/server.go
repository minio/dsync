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
	"log"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"
)

func StartLockServer(port int) {
	lockServer := &LockServer{
		resourceLockMapMutex: sync.Mutex{},
		resourceLockMap:      make(map[string][]int),
		sessionListMutex:     sync.Mutex{},
		sessionList:          make(map[string]struct{}),
	}

	portString := strconv.Itoa(port)
	rpcPath := serviceEndpointPrefix + portString

	rpcServer := rpc.NewServer()
	rpcServer.RegisterName("LockServer", lockServer)
	rpcServer.HandleHTTP(rpcPath, rpcPath)

	listener, err := net.Listen("tcp", ":"+portString)
	if err == nil {
		log.Println("LockServer listening at port", port, "under", rpcPath)
		http.Serve(listener, nil)
		// It never returns
	}

	log.Println("Unable to start LockServer on port", port, "under", rpcPath)
	log.Fatal("error:", err)
}

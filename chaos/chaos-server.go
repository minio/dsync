package main

import (
	"fmt"
	"github.com/minio/dsync"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"
	"time"
)

const LockMaintenanceLoop = 1 * time.Second
const LockCheckValidityInterval = 5 * time.Second

func startRPCServer(port int) {
	log.SetPrefix(fmt.Sprintf("[%d] ", port))
	log.SetFlags(log.Lmicroseconds)

	server := rpc.NewServer()
	locker := &lockServer{
		mutex:   sync.Mutex{},
		lockMap: make(map[string][]lockRequesterInfo),
	}
	go func() {
		// Start with random sleep time, so as to avoid "synchronous checks" between servers
		time.Sleep(time.Duration(rand.Float64() * float64(LockMaintenanceLoop)))
		for {
			time.Sleep(LockMaintenanceLoop)
			locker.lockMaintenance()
		}
	}()
	server.RegisterName("Dsync", locker)
	// For some reason the registration paths need to be different (even for different server objs)
	rpcPath := dsync.RpcPath + "-" + strconv.Itoa(port)
	server.HandleHTTP(rpcPath, fmt.Sprintf("%s-debug", rpcPath))
	l, e := net.Listen("tcp", ":"+strconv.Itoa(port))
	if e != nil {
		log.Fatal("listen error:", e)
	}
	log.Println("RPC server listening at port", port, "under", rpcPath)
	http.Serve(l, nil)
}

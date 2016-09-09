package main

import (
	"fmt"
	"github.com/minio/dsync"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"
	"time"
)

func startRPCServer(port int) {
	log.SetPrefix(fmt.Sprintf("[%d] ", port))
	log.SetFlags(log.Lmicroseconds)

	server := rpc.NewServer()
	locker := &lockServer{
		mutex:   sync.Mutex{},
		lockMap: make(map[string]int64),
	}
	go func() {
		for {
			time.Sleep(2 * time.Second)
			locker.check()
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

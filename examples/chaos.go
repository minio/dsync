package main

import (
	"fmt"
	"github.com/fwessels/dsync"
	"github.com/valyala/gorpc"
	"github.com/vburenin/nsync"
	"log"
	"strings"
	"time"
)

func startServer(port int, f func(clientAddr string, request interface{}, m *nsync.NamedMutex) interface{}) {

	m := nsync.NewNamedMutex()

	s := &gorpc.Server{
		// Accept clients on this TCP address.
		Addr: fmt.Sprintf(":%d", port),

		Handler: func(clientAddr string, request interface{}) interface{} {
			// Wrap handler function to pass in state
			return f(clientAddr, request, m)
		},
	}
	if err := s.Serve(); err != nil {
		log.Fatalf("Cannot start rpc server: %s", err)
	}
}

func handler(clientAddr string, request interface{}, m *nsync.NamedMutex) interface{} {

	parts := strings.Split(request.(string), "/")
	if parts[1] == "lock" {
		success := m.TryLockTimeout(parts[2], 1*time.Second)

		return fmt.Sprintf("%s/%v", strings.Join(parts, "/"), success)
	} else if parts[1] == "unlock" {

		m.Unlock(parts[2])

		return request
	} else {
		log.Println("Received unknown cmd", parts[1])
	}

	return request
}

func startServers() []string {

	nodes := make([]string, 8)

	for i := 0; i < len(nodes); i++ {
		nodes[i] = fmt.Sprintf("127.0.0.1:%d", i+12345)

		go startServer(i+12345, handler)
	}

	// Let servers start
	time.Sleep(10 * time.Millisecond)

	return nodes
}

func init() {

	nodes := startServers()
	dsync.SetNodes(nodes)
}

func main() {

	dm := dsync.DMutex{}
	dm.Lock()
	fmt.Println("locked")
	dm.Unlock()
	fmt.Println("unlocked")
}

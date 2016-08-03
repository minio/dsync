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
)

func startServer(port int, f func(clientAddr string, request interface{}, m *nsync.NamedMutex) interface{}) {

	m := nsync.NewNamedMutex()

	s := &gorpc.Server{
		// Accept clients on this TCP address.
		Addr: fmt.Sprintf(":%d", port),
		FlushDelay: time.Duration(-1),
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

	for run := 1; ; run++ {
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
	time.Sleep(10 * time.Millisecond)

}

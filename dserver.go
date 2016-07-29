package main

import (
	"fmt"
	"github.com/valyala/gorpc"
	"github.com/vburenin/nsync"
	"log"
	"time"
	"strings"
)

func startServer(port int) {

	//	lockMap := make(map[string]nsync.NamedMutex)
	m := nsync.NewNamedMutex()

	s := &gorpc.Server{
		// Accept clients on this TCP address.
		Addr: fmt.Sprintf(":%d", port),

		// Echo handler - just return back the message we received from the client
		Handler: func(clientAddr string, request interface{}) interface{} {
			log.Printf("Obtained request %+v from the client %s\n", request, clientAddr)

// 			cmd :=  // fmt.Sprintf("%+v", request)

			parts := strings.Split(request.(string), "/")
			if parts[1] == "lock" {
				m.Lock(parts[2])

				// lockMap["aap"] = m

				return request
			} else if parts[1] == "unlock" {

				m.Unlock(parts[2])
				//m, ok := lockMap["aap"]
				//if ok {
				//	delete(lockMap, "aap")
				//
				//	m.Unlock()
				//} else {
				//	log.Printf("Release lock received from unknown lock")
				//}

				return request
			} else {
				log.Println("Received unknown cmd", parts[1])
			}

			return request
		},
	}
	if err := s.Serve(); err != nil {
		log.Fatalf("Cannot start rpc server: %s", err)
	}
}

type Conn struct{}
type Result struct{}

func ReturnFirstResult(conns []Conn, query string) Result {
	ch := make(chan Result, 1)
	for _, conn := range conns {
		go func(c Conn) {
			select {
			// case ch <- c.DoQuery(query):
			default:
			}
		}(conn)
	}
	return <-ch
}

func TimeoutAfter() {

	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(1 * time.Second)
		timeout <- true
	}()

	ch := make(chan struct{})

	select {
	case <-ch:
	// a read from ch has occurred
	case <-timeout:
		// the read from ch has timed out
	}

}

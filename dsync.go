package dsync

import (
	"fmt"
	"time"
)

const N = 8

var nodes [N]string

func startServers() {

	for i := 0; i < N; i++ {
		nodes[i] = fmt.Sprintf("127.0.0.1:%d", i+12345)

		go startServer(i + 12345)
	}

	// Let servers start
	time.Sleep(10 * time.Millisecond)
}

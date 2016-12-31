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
	"bufio"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"sync"
	"time"

	"github.com/minio/dsync"
)

// defaultDialTimeout is used for non-secure connection.
const defaultDialTimeout = 3 * time.Second

// ReconnectRPCClient is a wrapper type for rpc.Client which provides reconnect on first failure.
type ReconnectRPCClient struct {
	mutex           sync.Mutex
	netRPCClient    *rpc.Client
	serverAddr      string
	serviceEndpoint string
	sessionID       string
}

// newReconnectRPCClient constructs a new ReconnectRPCClient object with given serverAddr
// and serviceEndpoint.  It does lazy connect to serverAddr on Call().
func newReconnectRPCClient(serverAddr, serviceEndpoint string) *ReconnectRPCClient {
	return &ReconnectRPCClient{
		serverAddr:      serverAddr,
		serviceEndpoint: serviceEndpoint,
	}
}

// dial tries to establish a connection to serverAddr in a safe manner.
// If there is a valid rpc.Cliemt, it returns that else creates a new one.
func (rpcClient *ReconnectRPCClient) dial() (*rpc.Client, error) {
	rpcClient.mutex.Lock()
	defer rpcClient.mutex.Unlock()

	// Nothing to do as we already have valid connection.
	if rpcClient.netRPCClient != nil {
		return rpcClient.netRPCClient, nil
	}

	// Dial with 3 seconds timeout.
	conn, err := net.DialTimeout("tcp", rpcClient.serverAddr, defaultDialTimeout)
	if err != nil {
		// Print RPC connection errors that are worthy to display in log
		switch err.(type) {
		case x509.HostnameError:
			log.Println("Unable to establish secure connection to", rpcClient.serverAddr)
			log.Println("error:", err)
		}
		return nil, &net.OpError{
			Op:   "dial-http",
			Net:  rpcClient.serverAddr + " " + rpcClient.serviceEndpoint,
			Addr: nil,
			Err:  err,
		}
	}

	io.WriteString(conn, "CONNECT "+rpcClient.serviceEndpoint+" HTTP/1.0\n\n")

	// Require successful HTTP response before switching to RPC protocol.
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == "200 Connected to Go RPC" {
		netRPCClient := rpc.NewClient(conn)
		if netRPCClient == nil {
			return nil, &net.OpError{
				Op:   "dial-http",
				Net:  rpcClient.serverAddr + " " + rpcClient.serviceEndpoint,
				Addr: nil,
				Err:  fmt.Errorf("Unable to initialize new rpc.Client"),
			}
		}

		rpcClient.netRPCClient = netRPCClient

		return netRPCClient, nil
	}

	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}

	conn.Close()
	return nil, &net.OpError{
		Op:   "dial-http",
		Net:  rpcClient.serverAddr + " " + rpcClient.serviceEndpoint,
		Addr: nil,
		Err:  err,
	}
}

// Call makes a RPC call to the remote endpoint using the default
// codec, namely encoding/gob.  It logs in automatically when session ID
// is empty.
func (rpcClient *ReconnectRPCClient) Call(serviceMethod string, args interface{}, reply interface{}) error {
	// Get a new or existing rpc.Client.
	netRPCClient, err := rpcClient.dial()
	if err != nil {
		return err
	}

	rpcClient.mutex.Lock()
	if rpcClient.sessionID == "" {
		loginArgs := NewLoginArgs("admin", "adm1npasswo7d")
		if err = netRPCClient.Call("LockServer.Login", &loginArgs, &rpcClient.sessionID); err != nil {
			rpcClient.mutex.Unlock()
			if err == rpc.ErrShutdown {
				rpcClient.Close()
			}
			return err
		}
	}
	rpcClient.mutex.Unlock()

	err = netRPCClient.Call(serviceMethod, args, reply)
	if err == rpc.ErrShutdown {
		netRPCClient.Close()
	}
	return err
}

func (rpcClient *ReconnectRPCClient) RLock(args dsync.LockArgs) (status bool, err error) {
	lockArgs := NewLockArgs(args, rpcClient.sessionID)
	err = rpcClient.Call("LockServer.RLock", &lockArgs, &status)
	if err == rpc.ErrShutdown {
		err = rpcClient.Call("LockServer.RLock", &lockArgs, &status)
	}
	return status, err
}

func (rpcClient *ReconnectRPCClient) Lock(args dsync.LockArgs) (status bool, err error) {
	lockArgs := NewLockArgs(args, rpcClient.sessionID)
	err = rpcClient.Call("LockServer.Lock", &lockArgs, &status)
	if err == rpc.ErrShutdown {
		err = rpcClient.Call("LockServer.Lock", &lockArgs, &status)
	}
	return status, err
}

func (rpcClient *ReconnectRPCClient) RUnlock(args dsync.LockArgs) (status bool, err error) {
	lockArgs := NewLockArgs(args, rpcClient.sessionID)
	err = rpcClient.Call("LockServer.RUnlock", &lockArgs, &status)
	if err == rpc.ErrShutdown {
		err = rpcClient.Call("LockServer.RUnlock", &lockArgs, &status)
	}
	return status, err
}

func (rpcClient *ReconnectRPCClient) Unlock(args dsync.LockArgs) (status bool, err error) {
	lockArgs := NewLockArgs(args, rpcClient.sessionID)
	err = rpcClient.Call("LockServer.Unlock", &lockArgs, &status)
	if err == rpc.ErrShutdown {
		err = rpcClient.Call("LockServer.Unlock", &lockArgs, &status)
	}
	return status, err
}

func (rpcClient *ReconnectRPCClient) ForceUnlock(args dsync.LockArgs) (status bool, err error) {
	lockArgs := NewLockArgs(args, rpcClient.sessionID)
	err = rpcClient.Call("LockServer.ForceUnlock", &lockArgs, &status)
	if err == rpc.ErrShutdown {
		err = rpcClient.Call("LockServer.ForceUnlock", &lockArgs, &status)
	}
	return status, err
}

// Close closes the underlying socket file descriptor.
func (rpcClient *ReconnectRPCClient) Close() (err error) {
	rpcClient.mutex.Lock()
	netRPCClient := rpcClient.netRPCClient
	rpcClient.mutex.Unlock()

	if netRPCClient != nil {
		var status bool
		netRPCClient.Call("LockServer.Logout", &rpcClient.sessionID, &status)

		rpcClient.mutex.Lock()
		rpcClient.netRPCClient = nil
		rpcClient.mutex.Unlock()

		netRPCClient.Close()
	}

	return nil
}

func (rpcClient *ReconnectRPCClient) ServerAddr() string {
	return rpcClient.serverAddr
}

func (rpcClient *ReconnectRPCClient) ServiceEndpoint() string {
	return rpcClient.serviceEndpoint
}

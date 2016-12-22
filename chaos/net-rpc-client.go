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
	"errors"
	"net/rpc"
	"sync"

	"github.com/minio/dsync"
)

// ReconnectRPCClient is a wrapper type for rpc.Client which provides reconnect on first failure.
type ReconnectRPCClient struct {
	mu         sync.Mutex
	rpcPrivate *rpc.Client
	node       string
	rpcPath    string
}

// newClient constructs a ReconnectRPCClient object with node and rpcPath initialized.
// It _doesn't_ connect to the remote endpoint. See Call method to see when the
// connect happens.
func newClient(node, rpcPath string) *ReconnectRPCClient {
	return &ReconnectRPCClient{
		node:    node,
		rpcPath: rpcPath,
	}
}

// clearRPCClient clears the pointer to the rpc.Client object in a safe manner
func (rpcClient *ReconnectRPCClient) clearRPCClient() {
	rpcClient.mu.Lock()
	rpcClient.rpcPrivate = nil
	rpcClient.mu.Unlock()
}

// getRPCClient gets the pointer to the rpc.Client object in a safe manner
func (rpcClient *ReconnectRPCClient) getRPCClient() *rpc.Client {
	rpcClient.mu.Lock()
	rpcLocalStack := rpcClient.rpcPrivate
	rpcClient.mu.Unlock()
	return rpcLocalStack
}

// dialRPCClient tries to establish a connection to the server in a safe manner
func (rpcClient *ReconnectRPCClient) dialRPCClient() (*rpc.Client, error) {
	rpcClient.mu.Lock()
	defer rpcClient.mu.Unlock()
	// After acquiring lock, check whether another thread may not have already dialed and established connection
	if rpcClient.rpcPrivate != nil {
		return rpcClient.rpcPrivate, nil
	}
	rpc, err := rpc.DialHTTPPath("tcp", rpcClient.node, rpcClient.rpcPath)
	if err != nil {
		return nil, err
	} else if rpc == nil {
		return nil, errors.New("No valid RPC Client created after dial")
	}
	rpcClient.rpcPrivate = rpc
	return rpcClient.rpcPrivate, nil
}

// Call makes a RPC call to the remote endpoint using the default codec, namely encoding/gob.
func (rpcClient *ReconnectRPCClient) Call(serviceMethod string, args interface{}, reply interface{}) error {
	// Make a copy below so that we can safely (continue to) work with the rpc.Client.
	// Even in the case the two threads would simultaneously find that the connection is not initialised,
	// they would both attempt to dial and only one of them would succeed in doing so.
	rpcLocalStack := rpcClient.getRPCClient()

	// If the rpc.Client is nil, we attempt to (re)connect with the remote endpoint.
	if rpcLocalStack == nil {
		var err error
		rpcLocalStack, err = rpcClient.dialRPCClient()
		if err != nil {
			return err
		}
	}

	// If the RPC fails due to a network-related error, then we reset
	// rpc.Client for a subsequent reconnect.
	err := rpcLocalStack.Call(serviceMethod, args, reply)
	if err != nil {
		if err.Error() == rpc.ErrShutdown.Error() {
			// Reset rpcClient.rpc to nil to trigger a reconnect in future
			// and close the underlying connection.
			rpcClient.clearRPCClient()

			// Close the underlying connection.
			rpcLocalStack.Close()

			// Set rpc error as rpc.ErrShutdown type.
			err = rpc.ErrShutdown
		}
	}
	return err
}

func (rpcClient *ReconnectRPCClient) RLock(args dsync.LockArgs) (status bool, err error) {
	err = rpcClient.Call("Dsync.RLock", &args, &status)
	return status, err
}

func (rpcClient *ReconnectRPCClient) Lock(args dsync.LockArgs) (status bool, err error) {
	err = rpcClient.Call("Dsync.Lock", &args, &status)
	return status, err
}

func (rpcClient *ReconnectRPCClient) RUnlock(args dsync.LockArgs) (status bool, err error) {
	err = rpcClient.Call("Dsync.RUnlock", &args, &status)
	return status, err
}

func (rpcClient *ReconnectRPCClient) Unlock(args dsync.LockArgs) (status bool, err error) {
	err = rpcClient.Call("Dsync.Unlock", &args, &status)
	return status, err
}

func (rpcClient *ReconnectRPCClient) ForceUnlock(args dsync.LockArgs) (status bool, err error) {
	err = rpcClient.Call("Dsync.ForceUnlock", &args, &status)
	return status, err
}

// Close closes the underlying socket file descriptor.
func (rpcClient *ReconnectRPCClient) Close() error {
	// See comment above for making a copy on local stack
	rpcLocalStack := rpcClient.getRPCClient()

	// If rpc client has not connected yet there is nothing to close.
	if rpcLocalStack == nil {
		return nil
	}

	// Reset rpcClient.rpc to allow for subsequent calls to use a new
	// (socket) connection.
	rpcClient.clearRPCClient()
	return rpcLocalStack.Close()
}

func (rpcClient *ReconnectRPCClient) ServerAddr() string {
	return rpcClient.node
}

func (rpcClient *ReconnectRPCClient) ServiceEndpoint() string {
	return rpcClient.rpcPath
}

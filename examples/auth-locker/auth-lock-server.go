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
	"math/rand"
	"sync"
)

const (
	defaultUsername = "admin"
	defaultPassword = "adm1npasswo7d"
	letterBytes     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

var validResources = []string{"Music", "Videos", "Downloads"}

func isValidResource(resource string) bool {
	for _, validResource := range validResources {
		if validResource == resource {
			return true
		}
	}

	return false
}

func genSessionID() string {
	b := make([]byte, 16)

	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}

	return "AUTHLOCKER-" + string(b)
}

type LockServer struct {
	resourceLockMapMutex sync.Mutex
	resourceLockMap      map[string][]int

	sessionListMutex sync.Mutex
	sessionList      map[string]struct{}
}

func (ls *LockServer) Login(args *LoginArgs, sessionID *string) error {
	if !(args.username == defaultUsername && args.password == defaultPassword) {
		return errors.New("Unknown username or password")
	}

	newSessionID := genSessionID()

	ls.sessionListMutex.Lock()
	defer ls.sessionListMutex.Unlock()

	if _, ok := ls.sessionList[newSessionID]; ok {
		return errors.New("Internal error.  Try again later")
	}

	ls.sessionList[newSessionID] = struct{}{}

	*sessionID = newSessionID

	return nil
}

func (ls *LockServer) Logout(sessionID *string, reply *bool) error {
	ls.sessionListMutex.Lock()
	defer ls.sessionListMutex.Unlock()

	if _, ok := ls.sessionList[*sessionID]; !ok {
		return errors.New("No such session ID")
	}

	delete(ls.sessionList, *sessionID)

	*reply = true

	return nil
}

func (ls *LockServer) isLoggedIn(sessionID string) bool {
	ls.sessionListMutex.Lock()
	defer ls.sessionListMutex.Unlock()

	_, ok := ls.sessionList[sessionID]
	return ok
}

func (ls *LockServer) RLock(args *LockArgs, reply *bool) error {
	if !ls.isLoggedIn(args.SessionID) {
		return errors.New("Not logged in")
	}

	if !isValidResource(args.LockArgs.Resource) {
		return errors.New("Invalid resource")
	}

	ls.resourceLockMapMutex.Lock()
	defer ls.resourceLockMapMutex.Unlock()

	counters, ok := ls.resourceLockMap[args.LockArgs.Resource]
	if !ok {
		counters = make([]int, 2)
	}

	counters[0]++
	ls.resourceLockMap[args.LockArgs.Resource] = counters

	*reply = true

	return nil
}

func (ls *LockServer) Lock(args *LockArgs, reply *bool) error {
	if !ls.isLoggedIn(args.SessionID) {
		return errors.New("Not logged in")
	}

	if !isValidResource(args.LockArgs.Resource) {
		return errors.New("Invalid resource")
	}

	ls.resourceLockMapMutex.Lock()
	defer ls.resourceLockMapMutex.Unlock()

	counters, ok := ls.resourceLockMap[args.LockArgs.Resource]
	if !ok {
		counters = make([]int, 2)
	}

	counters[1]++
	ls.resourceLockMap[args.LockArgs.Resource] = counters

	*reply = true

	return nil
}

func (ls *LockServer) RUnlock(args *LockArgs, reply *bool) error {
	if !ls.isLoggedIn(args.SessionID) {
		return errors.New("Not logged in")
	}

	if !isValidResource(args.LockArgs.Resource) {
		return errors.New("Invalid resource")
	}

	ls.resourceLockMapMutex.Lock()
	defer ls.resourceLockMapMutex.Unlock()

	counters, ok := ls.resourceLockMap[args.LockArgs.Resource]
	if !ok {
		return errors.New("No lock found")
	}

	if counters[0] <= 0 {
		*reply = false
		return nil
	}

	counters[0]--
	if counters[0] == 0 && counters[1] == 0 {
		delete(ls.resourceLockMap, args.LockArgs.Resource)
	} else {
		ls.resourceLockMap[args.LockArgs.Resource] = counters
	}

	*reply = true

	return nil
}

func (ls *LockServer) Unlock(args *LockArgs, reply *bool) error {
	if !ls.isLoggedIn(args.SessionID) {
		return errors.New("Not logged in")
	}

	if !isValidResource(args.LockArgs.Resource) {
		return errors.New("Invalid resource")
	}

	ls.resourceLockMapMutex.Lock()
	defer ls.resourceLockMapMutex.Unlock()

	counters, ok := ls.resourceLockMap[args.LockArgs.Resource]
	if !ok {
		return errors.New("No lock found")
	}

	if counters[1] <= 0 {
		*reply = false
		return nil
	}

	counters[1]--
	if counters[0] == 0 && counters[1] == 0 {
		delete(ls.resourceLockMap, args.LockArgs.Resource)
	} else {
		ls.resourceLockMap[args.LockArgs.Resource] = counters
	}

	*reply = true

	return nil
}

func (ls *LockServer) ForceUnlock(args *LockArgs, reply *bool) error {
	if !ls.isLoggedIn(args.SessionID) {
		return errors.New("Not logged in")
	}

	if !isValidResource(args.LockArgs.Resource) {
		return errors.New("Invalid resource")
	}

	ls.resourceLockMapMutex.Lock()
	defer ls.resourceLockMapMutex.Unlock()

	_, ok := ls.resourceLockMap[args.LockArgs.Resource]
	if !ok {
		return errors.New("No lock found")
	}

	*reply = false

	return errors.New("ForceUnlock is not supported")
}

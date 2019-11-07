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

import "github.com/minio/dsync/v3"

const serviceEndpointPrefix = "/lockserver-"

type LockArgs struct {
	dsync.LockArgs
	SessionID string
}

func NewLockArgs(args dsync.LockArgs, sessionID string) LockArgs {
	return LockArgs{args, sessionID}
}

type LoginArgs struct {
	username string
	password string
}

func NewLoginArgs(username, password string) LoginArgs {
	return LoginArgs{username, password}
}

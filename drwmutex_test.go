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

// GOMAXPROCS=10 go test

package dsync

import (
	"fmt"
	"time"
	"testing"
)

func TestSimpleWriteLock(t *testing.T) {

	drwm_r1 := NewDRWMutex("resource")
	drwm_r2 := NewDRWMutex("resource")
	drwm_w := NewDRWMutex("resource")

	drwm_r1.RLock()
	fmt.Println("1st read lock acquired, waiting...")

	drwm_r2.RLock()
	fmt.Println("2nd read lock acquired, waiting...")

	go func() {
		time.Sleep(1000 * time.Millisecond)
		drwm_r1.RUnlock()
		fmt.Println("1st read lock released, waiting...")
	}()

	go func() {
		time.Sleep(2000 * time.Millisecond)
		drwm_r2.RUnlock()
		fmt.Println("2nd read lock released, waiting...")
	}()

	fmt.Println("Trying to acquire write lock, waiting...")
	drwm_w.Lock()

	fmt.Println("Write lock acquired, waiting...")
	time.Sleep(2500 * time.Millisecond)

	drwm_w.Unlock()
}

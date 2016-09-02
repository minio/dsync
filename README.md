dsync
=====

A distributed sync package for Go.

Introduction
------------
 
`dsync` is a package for doing distributed locks over a network of `n` nodes. It is designed with simplicity in mind and hence offers limited scalability (`n <= 16`). Each node will be connected to all other nodes and lock requests from any node will be broadcast to all connected nodes. A node will succeed in getting the lock if `n/2 + 1` nodes (whether or not including itself) respond positively. If the lock is acquired it can be held for as long as the client desired and needs to be released afterwards. This will cause the release to be broadcast to all nodes after which the lock becomes available again.

Motivation
----------

This package was developed for the distributed server version of [Minio Object Storage](https://minio.io/). For this we needed a distributed locking mechanism for up to 16 servers that each would be running `minio server`. The locking mechanism itself should be a reader/writer mutual exclusion lock meaning that it can be held by a single writer or an arbitrary number of readers.

For [minio](https://minio.io/) the distributed version is started as follows (for a 6-server system):

```
$ minio server server1/disk server2/disk server3/disk server4/disk server5/disk server6/disk 
```
 
_(note that the same identical command should be run on servers `server1` through to `server6`)_

Design goals
------------

* **Simple design**: by keeping the design simple, many tricky edge cases can be avoided.
* **No master node**: there is no concept of a master node which, if this would be used and the master would be down, causes locking to come to a complete stop. (Unless you have a design with a slave node but this adds yet more complexity.)
* **Resilient**: if one or more nodes go down, the other nodes should not be affected and can continue to acquire locks (provided not more than `n/2 - 1` nodes are down).
* Drop-in replacement for `sync.RWMutex` and supports [`sync.Locker`](https://github.com/golang/go/blob/master/src/sync/mutex.go#L30) interface.
* Automatically reconnect to (restarted) nodes.

Restrictions
------------

* Limited scalability: up to 16 nodes.
* Fixed configuration: changes in the number and/or network names/IP addresses need a restart of all nodes in order to take effect.
* If a down node comes up, it will not try to (re)acquire any locks that it may have held.
* Not designed for high performance applications such as key/value stores.

Performance
-----------

* Support up to a total of 4000 locks/second for maximum size of 16 nodes (consuming 10% CPU usage per server) on moderately powerful server hardware.
* Lock requests (successful) should not take longer than 1ms (provided decent network connection of 1 Gbit or more between the nodes).

Usage
-----

### Exclusive lock 

Here is a simple example showing how to protect a single resource (drop-in replacement for `sync.Mutex`):

```
import (
    "github.com/minio/dsync"
)

func lockSameResource() {

    // Create distributed mutex to protect resource 'test'
	dm := dsync.NewDRWMutex("test")

	dm.Lock()
    log.Println("first lock granted")

	// Release 1st lock after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		log.Println("first lock unlocked")
		dm.Unlock()
	}()

    // Try to acquire lock again, will block until initial lock is released
    log.Println("about to lock same resource again...")
	dm.Lock()
    log.Println("second lock granted")

	time.Sleep(2 * time.Second)
	dm.Unlock()
}
```

which gives the following output:

```
2016/09/02 14:50:00 first lock granted
2016/09/02 14:50:00 about to lock same resource again...
2016/09/02 14:50:05 first lock unlocked
2016/09/02 14:50:05 second lock granted
```

### Read locks

DRWMutex also supports multiple simultaneous read locks as shown below (analogous to `sync.RWMutex`)

```
func twoReadLocksAndSingleWriteLock() {

	drwm := dsync.NewDRWMutex("resource")

	drwm.RLock()
	log.Println("1st read lock acquired, waiting...")

	drwm.RLock()
	log.Println("2nd read lock acquired, waiting...")

	go func() {
		time.Sleep(1 * time.Second)
		drwm.RUnlock()
		log.Println("1st read lock released, waiting...")
	}()

	go func() {
		time.Sleep(2 * time.Second)
		drwm.RUnlock()
		log.Println("2nd read lock released, waiting...")
	}()

	log.Println("Trying to acquire write lock, waiting...")
	drwm.Lock()
	log.Println("Write lock acquired, waiting...")
	
	time.Sleep(3 * time.Second)

	drwm.Unlock()
}
```

which gives the following output:

```
2016/09/02 15:05:20 1st read lock acquired, waiting...
2016/09/02 15:05:20 2nd read lock acquired, waiting...
2016/09/02 15:05:20 Trying to acquire write lock, waiting...
2016/09/02 15:05:22 1st read lock released, waiting...
2016/09/02 15:05:24 2nd read lock released, waiting...
2016/09/02 15:05:24 Write lock acquired, waiting...
```

Dealing with Stale Locks
------------------------

Known deficiencies
------------------

Server side logic
-----------------

On the server side just the following logic needs to be added (barring some extra error checking):

```
const WriteLock = -1

type lockServer struct {
	mutex   sync.Mutex
	lockMap map[string]int64 // Map of locks, with negative value indicating (exclusive) write lock
	                         // and positive values indicating number of read locks
}

func (l *lockServer) Lock(args *LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if _, *reply = l.lockMap[args.Name]; !*reply {
		l.lockMap[args.Name] = WriteLock // No locks held on the given name, so claim write lock
	}
	*reply = !*reply // Negate *reply to return true when lock is granted or false otherwise
	return nil
}

func (l *lockServer) Unlock(args *LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var locksHeld int64
	if locksHeld, *reply = l.lockMap[args.Name]; !*reply { // No lock is held on the given name
		return fmt.Errorf("Unlock attempted on an unlocked entity: %s", args.Name) 
	}
	if *reply = locksHeld == WriteLock; !*reply { // Unless it is a write lock
		return fmt.Errorf("Unlock attempted on a read locked entity: %s (%d read locks active)", args.Name, locksHeld)
	}
	delete(l.lockMap, args.Name) // Remove the write lock
	return nil
}
```

If you also want RLock()/RUnlock() functionality, then add this as well:

```
const ReadLock = 1

func (l *lockServer) RLock(args *LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var locksHeld int64
	if locksHeld, *reply = l.lockMap[args.Name]; !*reply {
		l.lockMap[args.Name] = ReadLock // No locks held on the given name, so claim (first) read lock
		*reply = true
	} else {
		if *reply = locksHeld != WriteLock; *reply { // Unless there is a write lock
			l.lockMap[args.Name] = locksHeld + ReadLock // Grant another read lock
		}
	}
	return nil
}

func (l *lockServer) RUnlock(args *LockArgs, reply *bool) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var locksHeld int64
	if locksHeld, *reply = l.lockMap[args.Name]; !*reply { // No lock is held on the given name
		return fmt.Errorf("RUnlock attempted on an unlocked entity: %s", args.Name)
	}
	if *reply = locksHeld != WriteLock; !*reply { // A write-lock is held, cannot release a read lock
		return fmt.Errorf("RUnlock attempted on a write locked entity: %s", args.Name)
	}
	if locksHeld > ReadLock {
		l.lockMap[args.Name] = locksHeld - ReadLock // Remove one of the read locks held
	} else {
		delete(l.lockMap, args.Name) // Remove the (last) read lock
	}
	return nil
}
```

Issues
------

* In case the node that has the lock goes down, the lock release will not be broadcast: what do we do? (periodically ping 'back' to requesting node from all nodes that have the lock?) Or detect that the network connection has gone down. 
* If one of the nodes that participated in the lock goes down, this is not a problem since (when it comes back online) the node that originally acquired the lock will still have it, and a request for a new lock will fail due to only `n/2` being available.
* If two nodes go down and both participated in the lock then there is a chance that a new lock will acquire locks from `n/2 + 1` nodes and will success, so we would have two concurrent locks. One way to counter this would be to monitor the network connections from the nodes that originated the lock, and, upon losing a connection to a node that granted a lock, get a new lock from a free node.  
* When two nodes want to acquire the same lock, it is possible for both to just acquire `n` locks and there is no majority winner so both would fail (and presumably fail back to their clients?). This then requires a retry in order to acquire the lock at a later time.
* What if late acquire response still comes in after lock has been obtained (quorum is in) and has already been released again. 



Extensions / Other use cases
----------------------------

### Robustness vs Performance

It is possible to trade some level of robustness with overall performance by not contacting each node for every Lock()/Unlock() cycle. In the normal case (example for `n = 16` nodes) a total of 32 RPC messages is sent and the lock is granted if at least a quorum of `n/2 + 1` nodes respond positively. When all nodes are functioning normally this would mean `n = 16` positive responses and, in fact, `n/2 - 1 = 7` responses over the (minimum) quorum of `n/2 + 1 = 9`. So you could say that this is some overkill, meaning that even if 6 nodes are down you still have an extra node over the quorum.

For this case it is possible to reduce the number of nodes to be contacted to for example `12`. Instead of 32 RPC messages now 24 message will be sent which is 25% less. As the performance is mostly depending on the number of RPC messages sent, the total locks/second handled by all nodes would increase by 33% (given the same CPU load).

You do however want to make sure that you have some sort of 'random' selection of which 12 out of the 16 nodes will participate in every lock. See [here](https://gist.github.com/fwessels/dbbafd537c13ec8f88b360b3a0091ac0) for some sample code that could help with this.

### Scale beyond 16 nodes?

Building on the previous example and depending on how resilient you want to be for outages of nodes, you can also go the other way, namely to increase the total number of nodes while keeping the number of nodes contacted per lock the same.

For instance you could imagine a system of 32 nodes where only a quorom majority of `9` would be needed out of `12` nodes. Again this requires some sort of pseudo-random 'deterministic' selection of 12 nodes out of the total of 32 servers (same [example](https://gist.github.com/fwessels/dbbafd537c13ec8f88b360b3a0091ac0) as above). 

Comparison to other techniques
------------------------------

We are well aware that there are more sophisticated systems such as zookeeper, raft, etc. However we found that for our limited use case this was adding too much complexity. So if `dsync` does not meet your requirements than you are probably better off using one of those systems.

License
-------

Released under the Apache License v2.0. You can find the complete text in the file LICENSE.

Contributing
------------

Contributions are welcome, please send PRs for any enhancements.

dsync
=====

A distributed sync package for Go.

Introduction
------------
 
`dsync` is a package for doing distributed locks over a network of `n` nodes. It is designed with simplicity in mind and hence offers limited scalability (`n <= 16`). Each node will be connected to all other nodes and lock requests from any node will be broadcast to all connected nodes. A node will succeed in getting the lock if `n/2 + 1` nodes (whether or not including itself) respond positively. If the lock is acquired it can be held for as long as the client desired and needs to be released afterwards. This will cause the release to be broadcast to all nodes after which the lock becomes available again.

Motivation
----------

This package was developed for the distributed server version of the [Minio Object Storage](https://minio.io/). For this we needed a distributed locking mechanism for up to 16 servers that each would be running `minio server`. The locking mechanism itself should be a reader/writer mutual exclusion lock meaning that it can be held by a single writer or an arbitrary number of readers.

In [minio](https://minio.io/) the distributed version is started as follows (for a 6-server system):

```
$ minio server server1/disk server2/disk server3/disk server4/disk server5/disk server6/disk 
```
 
_(note that the same identical command should be run on servers `server1` through to `server6`)_

Design goals
------------

* Implements [`sync/Locker`](https://github.com/golang/go/blob/master/src/sync/mutex.go#L30) interface (for exclusive lock)
* Simple design: by keeping the design simple, many tricky edge cases can be avoided.
* No master node: there is no concept of a master node which, if this would be used and the master would be down, causes locking to come to a complete stop. (Unless you have a design with a slave node but this adds yet more complexity.)
* Resilient: if one or more nodes go down, the other nodes should not be affected and can continue to acquire locks (provided not more than `n/2 - 1` nodes are down).
* Automatically reconnect to (restarted) nodes.

Restrictions
------------

* Limited scalability: up to 16 nodes.
* Fixed configuration: changes in the number and/or network names/IP addresses need a restart of all nodes in order to take effect.
* If a down node comes up, it will not in any way (re)acquire any locks that it may have held.
* Not designed for high performance applications such as key/value stores.

Performance
-----------

* Support up to a total of 4000 locks/second for maximum size of 16 nodes (consuming 10% CPU usage per server) on moderately powerful server hardware.
* Lock requests (successful) should not take longer than 1ms (provided decent network connection of 1 Gbit or more between the nodes).

Usage
-----

### Exclusive lock 

Here is a simple case showing how to protect a single resource (drop-in replacement for `sync/mutex`):

```
import (
    "github.com/minio/dsync"
)

func lockSameResource() {

    // Two locks on same resource
	dm1st := dsync.NewDRWMutex("test")
	dm2nd := dsync.NewDRWMutex("test")

	dm1st.Lock()
    log.Println("dm1st locked")

	// Release 1st lock after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		log.Println("dm1st unlocked")
		dm1st.Unlock()
	}()

    // Try to acquire 2nd lock, will block until 1st lock is released
    log.Println("about to lock dm2nd...")
	dm2nd.Lock()
    log.Println("dm2nd locked")

	time.Sleep(2 * time.Second)
	dm2nd.Unlock()
}
```

This gives the following results:

```
2016/09/01 15:53:03 dm1st locked
2016/09/01 15:53:03 about to lock dm2nd...
2016/09/01 15:53:08 dm1st unlocked
2016/09/01 15:53:09 dm2nd locked
```

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

Comparison to other techniques
------------------------------

We are well aware that there are more sophisticated systems such as zookeeper, raft, etc but we found that for our limited use case this was adding too much complexity. So if `dsync` does not meet your requirements than you are probably better off using one of those systems.

Extensions / Other use cases
----------------------------

Depending on your use case and how resilient you want to be for outages of servers, strictly speaking it is not necessarily needed to acquire a lock from all connected nodes. For instance one could imagine a system of 64 servers where only a quorom majority out of `8` would be needed (thus minimally 5 locks granted out of 8 servers). This would require some sort of pseudo-random 'deterministic' selection of 8 servers out of the total of 64 servers. A simple hashing of the resource name could be use to derive a deterministic set of 8 servers which would then be contacted.

License
-------

Released under the Apache License v2.0. You can find the complete text in the file LICENSE.

Contributing
------------

Contributions are welcome, please send PRs for any enhancements.

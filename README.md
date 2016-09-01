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

Here is a simple case showing how to protect one resource 

```
import (
    "github.com/minio/dsync
)

func locksSameResource() {

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
    log.Println("About to lock dm2nd...")
	dm2nd.Lock()
    log.Println("dm2nd locked")

	time.Sleep(2 * time.Second)
	dm2nd.Unlock()
}
```

```

```


Dealing with Stale Locks
------------------------

Known deficiencies
------------------

Server side logic
-----------------

On the server side the following logic 

```
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

Performance
-----------

```
benchmark                       old ns/op     new ns/op     delta
BenchmarkMutexUncontended-8     4.22          1164018       +27583264.93%
BenchmarkMutex-8                96.5          1223266       +1267533.16%
BenchmarkMutexSlack-8           120           1192900       +993983.33%
BenchmarkMutexWork-8            108           1239893       +1147949.07%
BenchmarkMutexWorkSlack-8       142           1210129       +852103.52%
BenchmarkMutexNoSpin-8          292           319479        +109310.62%
BenchmarkMutexSpin-8            1163          1270066       +109106.02%
```

License
-------

Released under the Apache License v2.0. You can find the complete text in the file LICENSE.

Contributing
------------

Contributions are welcome, please send PRs for any enhancements.

dsync
=====

A distributed sync package.

Introduction
------------

`dsync` is a package for doing distributed locks over a network ofi `n` nodes. It is designed with simplicity in mind and hence offers limited scalability (`n <= 16`). Each node will be connected to all other nodes and lock requests from any node will be broadcast to all connected nodes. A node will succeed in getting the lock if `n/2 + 1` nodes (including itself) respond positively. If the lock is acquired it can be held for some time and needs to be released afterwards. This will cause the release to be broadcast to all nodes after which the lock becomes available again.

Design goals
------------

* Simple design: by keeping the design simple, many tricky edge cases can be avoided.
* No master node: there is no concept of a master node which, if this would be used and the master would be down, causes locking to come to a complete stop. (Unless you have a design with a slave node but this adds yet more complexity.)
* Resilient: if one or more nodes go down, the other nodes should not be affected and can continue to acquire locks (provided not more than `n/2 - 1` nodes are down).
* Be able to find out which underlying nodes hold the lock for any acquired lock.
* Automatically reconnect to (restarted) nodes.
* Be as much as possible a drop-in replacement for `sync/mutex`.


Restrictions
------------

* Limited scalability: up to 16 nodes.
* Fixed configuration: changes in the number and/or network names/IP addresses need a full restart of all the nodes in order to take effect.


Issues
------

* In case the node that has the lock goes down, the release will not be broadcast: what do we do? (periodically ping 'back' to requesting node from all nodes that have the lock?)


Comparison to other techniques
------------------------------

We are well aware that there are more sophisticated systems such as zookeeper, raft, but we found that for our limited use case this was adding too much complexity. So if this does not meet your requirements than you are probably better off using one of those systems.
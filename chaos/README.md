
Chaos test for dsync
====================

This directory contains various sorts of 'chaos' tests for dsync. For instance it simulates the locking process is uneffected while servers that participate in the locking process go down and come back up again.

Tests
-----

This is a list of cases being tested:
- **`testNotEnoughServersForQuorum`**: verifies that when quorum cannot be achieved that locking will block
- **`testServerGoingDown`**: tests that a lock is granted when all servers are up, after too many servers die that a new lock will block and once servers are up again, the lock is granted
- **`testSingleServerOverQuorumDownDuringLock`**: verifies that if a server goes down while a lock is held, and comes back later another lock on the same name is not granted too early
- **`testSingleStaleLock`**: verifies that, despite a single stale lock, a new lock can still be acquired on same resource
- **`testMultipleStaleLocks`**: verifies that (before maintenance kicks in) multiple stale locks will prevent a new lock from being granted; and (after maintenance has happened) multiple stale locks not will prevent a new lock from being granted
- **`testClientThatHasLockCrashes`**: verifies that (after a lock maintenance loop) multiple stale locks will not prevent a new lock on same resource
- **`testTwoClientsThatHaveReadLocksCrash`**: like testClientThatHasLockCrashes but with two clients having read locks
- **`testWriterStarvation`**: tests that a separate implementation using a pair of two DRWMutexes can prevent writer starvation (due to too many read locks)

Known error cases
-----------------

- **`testMultipleServersOverQuorumDownDuringLockKnownError`**: verifies that if multiple servers go down while a lock is held, and come back later another lock on the same name is granted too early

Building
--------

```
$ cd chaos
$ go build
```
 
Running
-------

```
$ ./chaos
```

If it warns about the following

```
Found more than one chaos process. Killing all and exiting
```

it has found more than one chaos process (most likely a left over from a previous run of the program), simply repeat the `./chaos` command to try again.

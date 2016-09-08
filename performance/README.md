
Performance tests for dsync
===========================

This directory contains code for doing performance measurements for `dsync`.

You can either run it locally in a couple of terminals, or for a true test you can run it in the cloud on a bunch of servers. Depending on what you want to do you will need to configure (and build) the program differently since it needs the IP addresses for all the servers (nodes) participating in the locking process.

Building
--------

```
$ cd performance
$ go build
```

Running on localhost
--------------------

For a local test, you need to change the `nodes` array in `performance.go` to something like this (example for 4 terminal windows)

```
var nodes = []string{
	"127.0.0.1:12345",
	"127.0.0.1:12346",
	"127.0.0.1:12347",
	"127.0.0.1:12348"}
```

and `go build`, and then run the following in the first terminal 

```
$ ./performance -p 12345
```

followed by `./performance -p 12346` in the second terminal, etc. all the way through to `./performance -p 12348` for the last terminal.

Note that the actual test only starts once the program is started in all terminals.

Running in the cloud
--------------------

To measure the actual performance you will want to run it on a few servers in the cloud or on local servers. Again, you will need to change the `nodes` array in `performance.go` to something like the following with the actual IP addresses of the servers (imagine 8 servers)

```
var nodes = []string{
	"10.x0.y0.z0:12345",
	"10.x1.y1.z1:12346",
	"10.x2.y2.z2:12347",
	"10.x3.y3.z3:12348",
	"10.x4.y4.z4:12349",
	"10.x5.y5.z5:12350",
	"10.x6.y6.z6:12351",
	"10.x7.y7.z7:12352"}
```

Either build the `performance` program on each server or copy it over, and then run `./performance -p 12345` on the first server, `./performance -p 12346` on the second, etc. through to the last server. (Note that for this case you can set the port number to the same value.)

Performance 
-----------

See [here](https://github.com/minio/dsync#performance) for the collective results.

Tweaking
--------

By changing the number of parallel loops to get locks (`parallel := 5`) you can influence the overall CPU load.

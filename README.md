# [ALPHA] etcd-mesos
This is an Apache Mesos framework that runs an Etcd cluster.  It performs periodic health checks to ensure that the cluster has a stable leader and that raft is making progress.  It replaces nodes that die.

Guides:
* [Architecture](docs/architecture.md)
* [Administration](docs/administration.md)
* [Incident Response](docs/response.md)

## features

- [x] runs, monitors, and administers an etcd cluster of your desired size
- [x] recovers from n/2-1 failures by reconfiguring the etcd cluster and launching replacement nodes
- [x] optionally persists framework ID in zookeeper for framework failover purposes
- [x] reconstructs framework state during failover using surviving task metadata
- [x] etcd proxy configurer and optional SRV record support via mesos-dns
- [x] recovers from up to n-1 failures by picking a survivor to re-seed a new cluster (ranks survivors by raft index, prefering the replica with the highest commit)

## running
First, build the project:
```
make
```

The important binaries (`etcd-mesos-scheduler`, `etcd-mesos-proxy`, `etcd-mesos-executor`) are now present in the bin subdirectory.

A typical production invocation will look something like this:
```
/path/to/etcd-mesos-scheduler \
    -log_dir=/var/log/etcd-mesos \
    -master="zk://zk1:2181,zk2:2181,zk3:2181/mesos" \
    -cluster-name="mycluster" \
    -cluster-size=5 \
    -executor-bin=/path/to/etcd-mesos-executor \
    -etcd-bin=/path/to/etcd \
    -zk-framework-persist="zk://zk1:2181,zk2:2181,zk3:2181/etcd-mesos"
```

## service discovery
Options for finding your etcd nodes on mesos:

* Run the included proxy binary locally on systems that use etcd.  It retrieves the etcd configuration from mesos and starts an etcd proxy node.  Note that this it not a good idea on clusters with lots of tasks running, as the master will iterate through each task and spit out a fairly large chunk of JSON, so this approach should be avoided in favor of mesos-dns on larger clusters. 
```
etcd-mesos-proxy --master=zk://localhost:2181/mesos --cluster-name=mycluster
```

* Use mesos-dns, and have the etcd proxy use SRV discovery: 
```
etcd --proxy=on --discovery-srv=etcd-mycluster.mesos
```

* Use another system that builds configuration from mesos's state.json endpoint.  This is how #1 works, so check out the code for it in `cmd/etcd-mesos-proxy/app.go` if you want to go this route.  Be sure to minimize calls to the master for state.json on larger clusters, as this becomes an expensive operation that can easily DDOS your master if you are not careful.
* Current membership may be queried from the `etcd-mesos-scheduler`'s `/members` http endpoint that listens on the `--admin-port` (default 23400)

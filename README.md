# etcd-mesos
This is a mesos framework that spawns an etcd cluster on top of mesos.  It performs periodic health checks to ensure that the cluster has a stable leader and that raft is making progress.  It replaces nodes that die.

## running
First build the project:
```
make
```

The important binaries (`etcd-mesos-proxy`, `etcd-mesos-executor`, `etcd-mesos-scheduler`) are now present in the bin subdirectory.

It is strongly recommended to persist your framework ID into zk using the -zk-framework-persist flag.  This allows another instance of the etcd-mesos scheduler to take over during failover.  If this is not used, any etcd tasks started with a now-deceased etcd-mesos scheduler will be orphaned and must be manually terminated.  Further, it enforces uniquely named etcd clusters, which is extremely important if you are relying on systems that perform service discovery based on the name of a framework such as mesos-dns.

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
1. Run the included proxy binary locally on systems that use etcd.  It retrieves the etcd configuration from mesos and starts an etcd proxy node:
```
 etcd-mesos-proxy --master=zk://localhost:2181/mesos --cluster-name=mycluster
```
2. Use mesos-dns, and have the etcd proxy use SRV discovery:
```
etcd  --proxy=on --discovery-srv=etcd-mycluster.mesos
```
3. Use another system that builds configuration from mesos's state.json endpoint.  This is how #1 works, so check out the code for it in `cmd/etcd-mesos-proxy/app.go` if you want to go this route.

## status

- [x] recovers from n/2-1 failures by reconfiguring the etcd cluster and launching replacement nodes
- [x] optionally persists framework ID in zookeeper for framework failover purposes
- [x] reconstructs framework state during failover using surviving task metadata
- [x] proxy that configures itself based on cluster name
- [ ] recovers from up to n-1 failures by picking a survivor to re-seed a new cluster
- [ ] nodes periodically perform backup and send to an http endpoint, hdfs, or s3
- [ ] uses gang scheduling to improve cluster initialization

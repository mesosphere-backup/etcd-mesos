# etcd-mesos
This is a mesos framework that spawns an etcd cluster on top of mesos.  It performs periodic health checks to ensure that the cluster has a stable leader and that raft is making progress.  It replaces nodes that die.

## running
First build the project:
```
make
```

The important binaries are now present in the bin subdirectory.

```
Usage of bin/etcd_scheduler:
  -address="": Binding address for scheduler and artifact server
  -alsologtostderr=false: log to standard error as well as files
  -artifact-port=12300: Binding port for artifact server
  -cluster-name="default": Unique name of this etcd cluster
  -cluster-size=5: Total task count to run
  -decode-routines=1: Number of decoding routines
  -encode-routines=1: Number of encoding routines
  -etcd="./bin/etcd": Path to test executor
  -executor="./bin/etcd_executor": Path to test executor
  -failover-timeout-seconds=604800: Mesos framework failover timeout in seconds
  -httptest.serve="": if non-empty, httptest.NewServer serves on this address and blocks
  -log_backtrace_at=:0: when logging hits line file:N, emit a stack trace
  -log_dir="": If non-empty, write log files in this directory
  -logtostderr=false: log to standard error instead of files
  -master="127.0.0.1:5050": Master address <ip:port>
  -mesos-authentication-principal="": Mesos authentication principal
  -mesos-authentication-provider="SASL": Authentication provider to use, default is SASL that supports mechanisms: [CRAM-MD5]
  -mesos-authentication-secret-file="": Mesos authentication secret file
  -reseed-timeout=240: Seconds of etcd livelock to wait for before attempting a cluster re-seed
  -restore="": Local path or URI for an etcd backup to restore as a new cluster
  -send-routines=1: Number of network sending routines
  -single-instance-per-slave=true: Only allow one etcd instance to be started per slave
  -stderrthreshold=0: logs at or above this threshold go to stderr
  -v=0: log level for V logs
  -vmodule=: comma-separated list of pattern=N settings for file-filtered logging
  -zk-framework-persist="": Zookeeper URI of the form zk://host1:port1,host2:port2/chroot/path
```

It is optional, but recommended, to persist your framework ID into zk using the -zk-framework-persist flag.  This allows another instance of the etcd-mesos scheduler to take over during failover.  If this is not used, any etcd tasks started with a now-deceased etcd-mesos scheduler will be orphaned and must be manually terminated.
```
	etcd_scheduler -log_dir=/var/log/etcd-mesos \
		-master="zk://zk1:2181,zk2:2181,zk3:2181/mesos" \
		-cluster-name="mycluster" \
		-cluster-size=5 \
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

# [ALPHA] etcd-mesos
This is an Apache Mesos framework that runs an etcd cluster.  It performs periodic health checks to ensure that the cluster has a stable leader and that raft is making progress.  It replaces nodes that die.

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

Marathon spec:
```
{
  "id": "etcd",
  "container": {
    "docker": {
      "forcePullImage": true,
      "image": "mesosphere/etcd-mesos:0.1.0-alpha-target-23-24-25"
    },
    "type": "DOCKER"
  },
  "cpus": 0.2,
  "env": {
    "FRAMEWORK_NAME": "etcd",
    "WEBURI": "http://etcd.marathon.mesos:$PORT0/stats",
    "MESOS_MASTER": "zk://master.mesos:2181/mesos",
    "ZK_PERSIST": "zk://master.mesos:2181/etcd",
    "AUTO_RESEED": "true",
    "CLUSTER_SIZE": "3",
    "CPU_LIMIT": "1",
    "DISK_LIMIT": "4096",
    "MEM_LIMIT": "2048",
    "RESEED_TIMEOUT": "240",
    "VERBOSITY": "1"
  },
  "healthChecks": [
    {
      "gracePeriodSeconds": 60,
      "intervalSeconds": 30,
      "maxConsecutiveFailures": 0,
      "path": "/healthz",
      "portIndex": 0,
      "protocol": "HTTP"
    }
  ],
  "instances": 1,
  "mem": 128.0,
  "ports": [
    0,
    1,
    2
  ]
}
```

## building

```
make
```

The important binaries (`etcd-mesos-scheduler`, `etcd-mesos-proxy`, `etcd-mesos-executor`) are now present in the bin subdirectory.

A typical production invocation will look something like this:
```
/path/to/etcd-mesos-scheduler \
    -log_dir=/var/log/etcd-mesos \
    -master=zk://zk1:2181,zk2:2181,zk3:2181/mesos \
    -framework-name=etcd \
    -cluster-size=5 \
    -executor-bin=/path/to/etcd-mesos-executor \
    -etcd-bin=/path/to/etcd \
    -etcdctl-bin=/path/to/etcdctl \
    -zk-framework-persist=zk://zk1:2181,zk2:2181,zk3:2181/etcd-mesos
```

## service discovery
Options for finding your etcd nodes on mesos:

* Run the included proxy binary locally on systems that use etcd.  It retrieves the etcd configuration from mesos and starts an etcd proxy node.  Note that this it not a good idea on clusters with lots of tasks running, as the master will iterate through each task and spit out a fairly large chunk of JSON, so this approach should be avoided in favor of mesos-dns on larger clusters.
```
etcd-mesos-proxy --master=zk://localhost:2181/mesos --framework-name=etcd
```

* Use mesos-dns or another system that creates SRV records and have an etcd proxy use SRV discovery:
```
etcd --proxy=on --discovery-srv=etcd.mesos
```

* Use Mesos DNS or another DNS SRV system and have clients resolve `_etcd-server._client.<framework name>.mesos`

* Use another system that builds configuration from mesos's state.json endpoint.  This is how #1 works, so check out the code for it in `cmd/etcd-mesos-proxy/app.go` if you want to go this route.  Be sure to minimize calls to the master for state.json on larger clusters, as this becomes an expensive operation that can easily DDOS your master if you are not careful.
* Current membership may be queried from the `etcd-mesos-scheduler`'s `/members` http endpoint that listens on the `--admin-port` (default 23400)

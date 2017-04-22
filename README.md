# etcd-mesos

This is an [Apache Mesos](https://mesos.apache.org) framework for [etcd](https://github.com/coreos/etcd). It starts and manages the lifecycle of one highly-available `etcd` cluster of your desired size.

**WARNING**: this work is considered to be **`ALPHA`**!

## Features

* Start and manage the lifecycle of one highly-available `etcd` cluster of your desired size.
* Recover from n/2-1 failures by reconfiguring the `etcd` cluster and launching replacement members.
* Recover from up to n-1 simultaneous failures by picking a survivor to re-seed a new cluster (ranks survivors by [raft index](https://github.com/coreos/etcd/blob/master/raft/design.md), preferring the member with the most recent commit).
* Provide one `etcd` proxy (`etcd-mesos-proxy`) and optional SRV lookup support via [Mesos-DNS](https://github.com/mesosphere/mesos-dns).

For more information, please consult the following design documents:
* [Architecture](docs/architecture.md)
* [Administration](docs/administration.md)
* [Incident Response](docs/response.md)

## Deployment

There are quite a few different ways to run this framework but for the sake of simplicity let's deploy it as a [Marathon application](https://mesosphere.github.io/marathon/docs/application-basics.html):

```json
{
  "id":"etcd",
  "container":{
    "docker":{
      "image":"mesosphere/etcd-mesos:0.1.3",
      "forcePullImage":true
    },
    "type":"MESOS",
    "volumes":[]
  },
  "args":[],
  "cpus":0.2,
  "mem":128.0,
  "instances":1,
  "ports":[0, 0, 0],
  "healthChecks":[
    {
      "gracePeriodSeconds":60,
      "intervalSeconds":30,
      "maxConsecutiveFailures":0,
      "path":"/healthz",
      "portIndex":0,
      "protocol":"HTTP"
    }
  ],
  "env":{
    "FRAMEWORK_NAME":"etcd",
    "FRAMEWORK_FAILOVER_TIMEOUT_SECONDS":"300",
    "WEBURI":"http://etcd.marathon.mesos:$PORT0/stats",
    "CLUSTER_SIZE":"3",
    "MESOS_MASTER":"zk://localhost:2181/mesos",
    "ZK_PERSIST":"zk://localhost:2181/etcd-mesos",
    "VERBOSITY":"3",
    "SINGLE_INSTANCE_PER_SLAVE":"true",
    "AUTO_RESEED":"true",
    "RESEED_TIMEOUT":"240",
    "DISK_LIMIT":"1024",
    "CPU_LIMIT":"0.2",
    "MEM_LIMIT":"256"
  }
}
```

## Build (optional)

In order to build the code and, eventually, the Docker container image, one needs:
* `automake`, and
* either `go` (1.8.1 is the recommended version) and [`govendor`](https://github.com/kardianos/govendor), or
* Docker, in order to run the build in a container.

If all you want is to build the code, you have at least two ways to do it.

Use `go build`:
```sh
make
```

or use Docker:
```sh
make docker_build
```

All necessary binaries are now available in the `bin` subdirectory.

### Docker specifics

If you'd like to build a new Docker container image:
```sh
make docker
```

Optionally, set `DOCKER_ORG` and `VERSION`:
```
make docker DOCKER_ORG=quay.io/myorg VERSION=myver
```

Optionally, push the container image:
```sh
 make docker DOCKER_ORG=quay.io/myorg VERSION=myver DOCKER_PUSH_ENABLED=1
```

## Service discovery

Below, you'll find some options for finding your `etcd` members:

* Run the included proxy binary locally on systems that use `etcd`.  This proxy retrieves the `etcd` configuration from Mesos and starts one `etcd` proxy member.

```sh
etcd-mesos-proxy --master=zk://localhost:2181/mesos --framework-name=etcd
```

**Note** though, that this should be avoided in large clusters, running lots of tasks, given the master will iterate through all tasks and spit out a fairly large chunk of JSON. In such scenarios, Mesos-DNS is recommended.

* Use Mesos-DNS or similar solutions that manage SRV records, and have the `etcd` proxy rely on SRV discovery:

```sh
etcd --proxy=on --discovery-srv=etcd.mesos
```

* Use Mesos-DNS or similar solutions that manage SRV records, and have clients resolve `_etcd-server._client.<framework name>.mesos`

* Use another solution that builds configuration from [Mesos' master state endpoint](http://mesos.apache.org/documentation/latest/endpoints/master/state/). This is how the first option works, so check out the [`etcd-mesos-proxy`](https://github.com/mesosphere/etcd-mesos/blob/master/cmd/etcd-mesos-proxy/app.go) code if you want to pursuit this route. Just make sure to minimize calls to the master state on larger clusters, as this becomes quite an expensive operation that can easily [DDoS](https://en.wikipedia.org/wiki/Denial-of-service_attack) your cluster.

* Also, current membership may be queried from the `etcd-mesos-scheduler`'s `/members` HTTP endpoint, available through the `--admin-port` (defaults to `23400`).

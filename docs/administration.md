# Administration
First, familiarize yourself with the [architecture doc](architecture.md).

## Dependencies
### Hard Dependencies
* [Apache Mesos](https://mesos.apache.org/)

### Strongly Recommend Dependencies
* [Apache Zookeeper](https://zookeeper.apache.org/) required for HA Mesos and HA etcd-mesos scheduler

### Optional Systems
* [Marathon](https://github.com/mesosphere/marathon)/[Kubernetes](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/docs/getting-started-guides/mesos.md)/[Aurora](https://github.com/apache/aurora)/etc... for managing the etcd-mesos scheduler process.
* [Mesos-DNS](https://github.com/mesosphere/mesos-dns) for SRV record discovery of Etcd instances running on etcd-mesos.
* [etcd-dump](https://github.com/AaronO/etcd-dump) for performing backups and restores of non-recomputable data

## Deployment

A basic production invocation will look something like this:
```
/path/to/etcd-mesos-scheduler \
    -log_dir=/var/log/etcd-mesos \
    -master="zk://zk1:2181,zk2:2181,zk3:2181/mesos" \
    -cluster-name="mycluster" \
    -cluster-size=3 \
    -executor-bin=/path/to/etcd-mesos-executor \
    -etcd-bin=/path/to/etcd \
    -zk-framework-persist="zk://zk1:2181,zk2:2181,zk3:2181/etcd-mesos"
```

Important tunables for you to select:

1. `-cluster-size` should be 3, 5, or (in rare low-write high-read cases) 7.  More nodes gets you more fault tolerance, better read performance, but worse write performance.
2. `-auto-reseed` (defaults to true) determines whether etcd-mesos will perform automatic cluster reseeding when a livelock has been going on for a configurable window.  See the "Mesos Slave" section of the [architecture doc](architecture.md) for a more in-depth description of what reseeding entails.  The summary is: disable this if you are willing to see higher MTTR so that a human is always in the loop to determine whether to reseed or not.


## Monitoring
The `etcd-mesos-scheduler` may be monitored by periodically querying the `/stats` endpoint (see HTTP Admin Interface below).  It is recommended that you periodically collect this in an external time-series database which is monitored by an alerting system.  Of particular interest are the counters for `failed_servers`, `cluster_livelocks`, `cluster_reseeds`, and `healthy`.  Healthy should be 1 if true, and 0 if the cluster is currently livelocked.

See the [architecture doc](architecture.md) for a summary of how the `healthy` field is determined.

## HTTP Admin Interface
The `etcd-mesos-scheduler` exposes a simple administration interface on the `--admin-port` (defaulting to 23400) which can handle:
* `/stats` returns a JSON map of basic statistics.  Note that counters are reset when an `etcd-mesos-scheduler` process is started.
* `/membership` returns a JSON list of current etcd servers.
* `/reseed` Manually triggers a cluster reseed.  Use extreme caution!

# Administration
First, familiarize yourself with the [architecture doc](architecture.md).

## Dependencies
### Hard Dependencies
* Apache Mesos

### Strongly Recommend Dependencies
* Apache Zookeeper - required for HA Mesos and HA etcd-mesos scheduler

### Optional Systems
* Marathon/Kubernetes/Aurora/etc... - for managing the etcd-mesos scheduler process.
* Mesos-DNS - for SRV record discovery of Etcd instances running on etcd-mesos.

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

## Monitoring
The `etcd-mesos-scheduler` may be monitored by periodically querying the `/stats` endpoint (see HTTP Admin Interface below).  It is recommended that you periodically collect this in an external time-series database which is monitored by an alerting system.  Of particular interest are the counters for `failed_servers`, `cluster_livelocks`, `cluster_reseeds`, and `healthy`.  Healthy should be 1 if true, and 0 if the cluster is currently livelocked.

## HTTP Admin Interface
The `etcd-mesos-scheduler` exposes a simple administration interface on the `--admin-port` (defaulting to 23400) which can handle:
* `/stats` returns a JSON map of basic statistics.  Note that counters are reset when an `etcd-mesos-scheduler` process is started.
* `/membership` returns a JSON list of current etcd servers.
* `/reseed` Manually triggers a cluster reseed.  Use extreme caution!

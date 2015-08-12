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


## Architecture and Fault Tolerance
```
                               +--+
         +-------------------->|ZK|<-------------------+
         |                     +--+                    |
         |                       ^                     |
  +-------------+                |          +--------------------+
  |mesos master |<---------------+--------->|etcd-mesos-scheduler|
  +-------------+                |          +--------------------+
         ^                       |                     |
         |                       |                     |
         +-+---------------------+---------------------+
           |                     |                     |
           v                     v                     v
 +-------------------+ +-------------------+ +-------------------+
 |    mesos slave    | |    mesos slave    | |    mesos slave    |
 +-------------------+ +-------------------+ +-------------------+
 |etcd-mesos-executor| |etcd-mesos-executor| |etcd-mesos-executor|
 +-------------------+ +-------------------+ +-------------------+
 |       etcd        | |       etcd        | |       etcd        |
 +-------------------+ +-------------------+ +-------------------+
           ^                     ^                     ^
           |                     |                     |
           +------------etcd communication ------------+
                                 |
                 +--------------------------------+
                 |etcd-mesos-proxy, normal clients|
                 +--------------------------------+
```
### Responsibilities
* zookeeper stores the host and port for the current mesos master, as well as the etcd-mesos-scheduler framework ID
* mesos master passes slave resource offers and etcd task status updates between the slave and the etcd-mesos-scheduler
* etcd-mesos-scheduler picks slaves to run etcd on, monitors their health, and repairs the cluster when necessary
* etcd-mesos-executor starts etcd and assists the scheduler in performing cluster maintenance
* etcd-mesos-proxy is a normal etcd proxy that has been configured to communicate with etcd running on mesos

### Failure Responses
#### ZK
* If a mesos master loses connectivity to ZK, it will commit suicide as it can no longer guarantee that it is the authoritative master.
* If a mesos slave loses connectivity to ZK, it will continue running tasks until ___???___
* If the etcd-mesos-scheduler loses connectivity to ZK, it will continue running as long as the master stays up.
* Etcd continues to run until the slave ___???___
#### Mesos Master
* If a mesos slave loses connectivity to the mesos master, it will buffer task updates and wait for a master to become available again.  All status updates pass through the mesos master, so they will not reach the etcd-mesos scheduler until a new master comes online.
* If the etcd-mesos-scheduler loses connectivity to the mesos master, it will put itself into an inactive state until it regains connectivity to a master.
* Etcd continues to run until the slave ___???___
#### `etcd-mesos-scheduler`
* Mesos slaves will continue running (unmanaged) Etcd tasks for up to 1 week (overridable with the scheduler flag `--failover-timeout-seconds`) until the framework times out and transitions to the `COMPLETED` state.

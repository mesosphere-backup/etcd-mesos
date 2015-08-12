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
                 +-------------------------------+
                 |etcd-mesos-proxy, other clients|
                 +-------------------------------+
```
### Responsibilities
* zookeeper stores the host and port for the current mesos master, as well as the etcd-mesos-scheduler framework ID
* mesos master passes slave resource offers and etcd task status updates between the slave and the etcd-mesos-scheduler
* etcd-mesos-scheduler picks slaves to run etcd on, monitors their health, and repairs the cluster when necessary.
* etcd-mesos-executor starts etcd and assists the scheduler in performing cluster maintenance. This is started automatically on nodes that the etcd-mesos-scheduler has chosen for running Etcd on.
* etcd-mesos-proxy is a normal etcd proxy that has been configured to communicate with etcd running on mesos


### Health Checking
Etcd-mesos performs a health check that attempts to connect to each known Etcd server and query the current Raft commit and leadership term indices.  If the Raft commit index fails to increase, it means that the leader is not successfully performing AppendEntries RPC's.  If the Raft leadership term is increasing, it means that nodes are failing to hear from a leader for long enough to trigger a leader election, which effectively dethrones the previous leader if a majority of live nodes receive the RequestVotes RPC from a candidate.

(bonus: It's possible for network partitions to trigger this situation fairly easily, and if a partition arises between certain nodes but not between others, it's possible for candidates to alternatively dethrone each other until the partition heals.)

No Etcd nodes will be launched unless a cluster has been determined to be in a healthy state.

### Failure Responses
#### ZK
* If a mesos master loses connectivity to ZK for its `--zk_session_timeout` parameter (default 10s), it will commit suicide as it can no longer guarantee that it is the authoritative master.
* If the etcd-mesos-scheduler loses connectivity to ZK, it will continue running as long as the master stays up.


#### Mesos Master
* If a mesos slave loses connectivity to the mesos master, it will buffer task updates and wait for a master to become available again.  All status updates pass through the mesos master, so they will not reach the etcd-mesos scheduler until a new master comes online.
* If the etcd-mesos-scheduler loses connectivity to the mesos master, it will put itself into an "Immutable" state that prevents launching new Etcd services until it regains connectivity to a master.
* Etcd continues to run as long as the slaves don't lose connectivity with the master for the master's `--slave_reregister_timeout` (default 10m)


#### `etcd-mesos-scheduler`
* Mesos slaves will continue running (unmanaged by etcd-mesos) Etcd tasks for up to 1 week (overridable with the scheduler flag `--failover-timeout-seconds`) until the framework times out and transitions to the `COMPLETED` state.


#### Mesos Slave
* If a mesos slave is lost for the master's `--slave_reregister_timeout` (default 10m), the mesos master will send a message to the `etcd-mesos-scheduler` that the slave has been lost.  The `etcd-mesos-scheduler` will remove that node from the set of alive nodes.  The next time the mesos master sends the `etcd-mesos-scheduler` a sufficient offer, a new etcd server will be started and the cluster will be configured for it to join.
* If up to N/2-1 (where N is the `--cluster-size` argument passed to the scheduler) mesos slaves are lost, the above response occurs for each.
* If a majority of Etcd servers are lost, etcd will livelock, as all key and membership changes occur over Raft itself, which requires a simple majority of peers to agree.  If there were 5 nodes total, and 3 have died, no majority may be reached.  By default, the `etcd-mesos-scheduler` will wait `--reseed-timeout` seconds for the cluster to recover.  If it does not, it initiates a reseed event.  Reseeding involves querying each live Etcd server to determine its Raft index, and attempts to restart the most up-to-date Etcd server with the `--force-new-cluster` flag to allow it to become the leader of a new cluster.  This can be disabled by passing `--auto-reseed=false` to the `etcd-mesos-scheduler`.  A cluster may be manually reseeded by GET'ing `http://<host of etcd-mesos-scheduler>:<admin-port>/reseed` if `--auto-reseed=false`, and some users will prefer to manually react to this operation, as it involves riskier operations than recovery of a minority of slave failures.

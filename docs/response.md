# On-Call Response Guide

Information Sources:

1. Mesos Master web UI: `http://<host of mesos master>:<port usually 5050>/#/frameworks`, go to the <--framework-name> framework and you will see the running tasks.  Tasks are named by `<etcd ident> <hostname> <etcd peer port> <etcd client port> <etcd-mesos reseed listener port>` 
2. Etcd-mesos list of running etcd servers: `http://<host of etcd-mesos-scheduler>:<admin-port, 23400 by default>/members` for a list of currently running members in a format similar to above
3. Etcd-mesos operational stats: `http://<host of etcd-mesos-scheduler>:<admin-port, 23400 by default>/stats` for counters about running/lost nodes, livelock events, reseed attempts, whether the cluster is healthy in the scheduler's opinion (1 is healthy, 0 is unhealthy)
4. Etcd health check: `etcdctl -C http://<one of the hosts from the member list above>:<etcd client port for it> cluster-health`
5. Etcd membership check: `etcdctl -C http://<one of the hosts from the member list above>:<etcd client port for it> member list`

Tools for Interaction:

1. Manual reseed trigger: `http://<host of etcd-mesos-scheduler>:<admin-port, 23400 by default>/reseed` (use only in extreme situations when there has been a catastrophic loss of up to N-1 servers)

Finding your data:

1. Find the host of your slave by going to the mesos master web ui (see number 1 in Information Sources above)
2. Click on the "sandbox" link for one of the slaves
3. The path for the task's sandbox on the slave is visible near the top of the page

## Types of possible incidents:
#### Etcd livelock
Livelock occurs when a majority of the Etcd cluster has been lost.  This prevents all writes and incremental membership changes to etcd.

If a majority of nodes have been lost, unless the `--auto-reseed=false` flag been passed to the scheduler, the scheduler will perform an automatic reseed attempt after `reseed-timeout` seconds.

If this has been disabled, you may manually trigger a reseed attempt by HTTP GET'ing the `/reseed` path as seen on #1 in "Tools for Interaction" above.

#### Total Cluster Loss
If all members of a cluster have been lost, and etcd was storing non-recomputable data, you must retrieve a previous replica's data from the mesos slave sandbox or restore from a previous backup.  Mesos tasks store their data in the mesos slave's work directory, but this discarded after some time, so you need to retrieve old data directories quickly.  Steps:
The etcd-mesos scheduler locks when it detects a total cluster loss, preventing it from launching any more tasks.  If you require instant restoration of writes to a fresh cluster, restart the scheduler process.
To restore data from a lost cluster:
1. In the mesos UI (#1 in Information Sources above), find the etcd framework, and then the tasks that were lost.
2. Log into a slave where they ran, and visit the directory shown at the top of the mesos UI page for the task
3. Copy the etcd_data directory to a location outside of the mesos slave's work directory, as this directory will be destroyed automatically
4. Start etcd on the default ports (it doesn't matter unless you're already running something on those ports) and supply `--data-dir=./etcd_data` and `--force-new-cluster` as arguments so that it ignores previous member information
5. Use a tool like [etcd-backup](https://github.com/fanhattan/etcd-backup) to retrieve a remotely-restorable copy of the dataset
6. Restart the etcd-mesos scheduler.  This will create a new cluster from scratch.
7. Use the backup tool from #5 to restore the dataset onto the new cluster.

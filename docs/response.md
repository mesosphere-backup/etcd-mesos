# On-Call Response Guide

Information Sources:
1. Mesos Master web UI: `http://<host of mesos master>:<port usually 5050>/#/frameworks`, go to the etcd-<cluster-name> framework and you will see the running tasks.  Tasks are named by `<etcd ident> <hostname> <etcd peer port> <etcd client port> <etcd-mesos reseed listener port>` 
2. Etcd-mesos running etcd server info: `http://<host of etcd-mesos-scheduler>:<port, 12300 by default>/members` for a list of currently running members in a format similar to above
3. Etcd-mesos operational stats: `http://<host of etcd-mesos-scheduler>:<port, 12300 by default>/stats` for counters about running/lost nodes, livelock events, reseed attempts, whether the cluster is healthy in the scheduler's opinion (1 is healthy, 0 is unhealthy)
4. Etcd health check: `etcdctl -C http://<one of the hosts from the member list above>:<etcd client port for it> cluster-health`
5. Etcd membership check: `etcdctl -C http://<one of the hosts from the member list above>:<etcd client port for it> member list`

Tools for Interaction:
1. Manual reseed trigger: `http://<host of etcd-mesos-scheduler>:<port, 12300 by default>/reseed` (use only in extreme situations when there has been a catastrophic loss of up to N-1 servers)

Finding your data:
1. Find the host of your slave by going to the mesos master web ui (see number 1 in Information Sources above)
2. Click on the "sandbox" link for one of the slaves
3. The path for the task's sandbox on the slave is visible near the top of the page

## Types of possible incidents:
#### Etcd livelock
Livelock occurs when a majority of the Etcd cluster has been lost.  This prevents all writes and incremental membership changes to etcd.

If mesos tasks for the unreachable Etcd servers are in "TASK_LOST" state, you should not expect them to come back.

If a majority of nodes have been lost, and the `--auto-reseed=false` flag has not been passed to the scheduler, the scheduler will perform an automatic reseed attempt after `reseed-timeout` seconds.

If this has been disabled, you may manually trigger an attempt by HTTP GET'ing the `/reseed` path as seen on #1 in "Tools for Interaction" above.

#### Total Cluster Loss
If all members of a cluster have been lost, and etcd was storing non-recomputable data, you must restore from a backup.  Periodic backups are recommended if you are using etcd to store data that cannot be recomputed/replaced/reconfigured in the event of loss.  Tools such as [etcd-dump](https://github.com/AaronO/etcd-dump) may be of use to you, but this is not currently handled by etcd-mesos.

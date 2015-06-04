# etcd-mesos
etcd on mesos

- [x] recovers from n/2-1 failures by reconfiguring the etcd cluster and launching replacement nodes
- [x] optionally persists framework ID in zookeeper for framework failover purposes
- [x] reconstructs framework state during failover using surviving task metadata
- [ ] nodes periodically perform backup and send to an http endpoint, hdfs, or s3
- [ ] recovers from up to n-1 failures by performing a backup and reinitializing the cluster from that image
- [ ] support for spawning proxy nodes
- [ ] uses gang scheduling to improve cluster initialization

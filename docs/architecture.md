## architecture
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
                        +----------------+                        
                        |etcd-mesos-proxy|                        
                        +----------------+                                
```
Responsibilities:
* zookeeper stores the host and port for the current mesos master, as well as the etcd-mesos-scheduler framework ID
* mesos master passes slave resource offers and etcd task status updates between the slave and the etcd-mesos-scheduler
* etcd-mesos-scheduler picks slaves to run etcd on, monitors their health, and repairs the cluster when necessary
* etcd-mesos-executor starts etcd and assists the scheduler in performing cluster maintenance
* etcd-mesos-proxy is a normal etcd proxy that has been configured to communicate with etcd running on mesos

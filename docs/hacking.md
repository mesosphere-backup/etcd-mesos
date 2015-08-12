# Dev Guide
It's supremely important to run the scheduler on "real" clusters to sanity check your code changes, even if test coverage supposedly covers these cases.

#### Running Mesos
Run each of the following in separate terminals for easy killing and log monitoring.  The for-loop is for simulating total slave loss, and will cause etcd to die when its data files are nuked in the slave's sandbox.
```
zkServer.sh start-foreground
/usr/local/sbin/mesos-master --ip=127.0.0.1 --hostname="localhost" --work_dir=/tmp/mesos-master --zk="zk://localhost:2181/mesos" --quorum=1
while true; do rm -rf /tmp/mesos-slave1;  /usr/local/sbin/mesos-slave --port=5051 --work_dir=/tmp/mesos-slave1 --resources="cpus:4;mem:4096;disk:4096;ports:[30000-31000]"  --master="zk://localhost:2181/mesos" --containerizers=docker,mesos; done
while true; do rm -rf /tmp/mesos-slave2;  /usr/local/sbin/mesos-slave --port=5052 --work_dir=/tmp/mesos-slave2 --resources="cpus:4;mem:4096;disk:4096;ports:[31000-32000]"  --master="zk://localhost:2181/mesos" --containerizers=docker,mesos; done
while true; do rm -rf /tmp/mesos-slave3;  /usr/local/sbin/mesos-slave --port=5053 --work_dir=/tmp/mesos-slave3 --resources="cpus:4;mem:4096;disk:4096;ports:[32000-33000]"  --master="zk://localhost:2181/mesos" --containerizers=docker,mesos; done
while true; do rm -rf /tmp/mesos-slave4;  /usr/local/sbin/mesos-slave --port=5054 --work_dir=/tmp/mesos-slave4 --resources="cpus:4;mem:4096;disk:4096;ports:[33000-34000]"  --master="zk://localhost:2181/mesos" --containerizers=docker,mesos; done
while true; do rm -rf /tmp/mesos-slave5;  /usr/local/sbin/mesos-slave --port=5055 --work_dir=/tmp/mesos-slave5 --resources="cpus:4;mem:4096;disk:4096;ports:[34000-35000]"  --master="zk://localhost:2181/mesos" --containerizers=docker,mesos; done
```

Generally, I will run a local etcd-mesos scheduler by invoking `make run-scheduler-with-zk`.

#### Key Tests
###### loss of (n-1)/2 nodes (while scheduler has been killed)
1. shut down scheduler
2. restart a minority of mesos slaves
3. bring up scheduler
4. verify that the cluster returns to the desired number of nodes

###### loss of n/2+1 nodes triggers reseed
1. restart a majority of mesos slaves
2. wait for the reseed-timeout to elapse (pass in a low number of seconds to speed this up)
3. verify that the cluster reseeds and returns to the desired number of nodes

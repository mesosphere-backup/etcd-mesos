FROM debian
ADD . /work
WORKDIR /work
ENV CLUSTER_NAME=1
ENV CLUSTER_SIZE=5
ENV MESOS_MASTER=zk://localhost:2181/mesos
ENV ZK_PERSIST=zk://localhost:2181/etcd-mesos
ENV VERBOSITY=1
ENV AUTO_RESEED=true
ENV RESEED_TIMEOUT=240
ENV DISK_LIMIT=4096
ENV CPU_LIMIT=4
ENV MEM_LIMIT=2048
CMD sh -c '/work/bin/etcd-mesos-scheduler -alsologtostderr=true \
    -cluster-name=${CLUSTER_NAME} \
    -cluster-size=${CLUSTER_SIZE} \
    -master=${MESOS_MASTER} \
    -zk-framework-persist=${ZK_PERSIST} \
    -v=${VERBOSITY} \
    -auto-reseed=${AUTO_RESEED} \
    -reseed-timeout=${RESEED_TIMEOUT} \
    -sandbox-disk-limit=${DISK_LIMIT} \
    -sandbox-cpu-limit=${CPU_LIMIT} \
    -sandbox-mem-limit=${MEM_LIMIT} \
    -admin-port=${PORT0} \
    -driver-port=${PORT1} \
    -artifact-port=${PORT2}'

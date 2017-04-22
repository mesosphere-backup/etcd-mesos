FROM debian

ADD bin /work/bin
ADD static /work/static

WORKDIR /work

ENV FRAMEWORK_NAME=etcd
ENV FRAMEWORK_FAILOVER_TIMEOUT_SECONDS=60*60*24*7
ENV CLUSTER_SIZE=5
ENV MESOS_MASTER=zk://localhost:2181/mesos
ENV ZK_PERSIST=zk://localhost:2181/etcd-mesos
ENV VERBOSITY=1
ENV SINGLE_INSTANCE_PER_SLAVE=true
ENV AUTO_RESEED=true
ENV RESEED_TIMEOUT=240
ENV DISK_LIMIT=4096
ENV CPU_LIMIT=1
ENV MEM_LIMIT=1024

CMD sh -xc "/work/bin/etcd-mesos-scheduler -alsologtostderr=true \
    -address=${LIBPROCESS_IP} \
    -framework-name=${FRAMEWORK_NAME} \
    -failover-timeout-seconds=${FRAMEWORK_FAILOVER_TIMEOUT_SECONDS} \
    -cluster-size=${CLUSTER_SIZE} \
    -master=${MESOS_MASTER} \
    -zk-framework-persist=${ZK_PERSIST} \
    -v=${VERBOSITY} \
    -single-instance-per-slave=${SINGLE_INSTANCE_PER_SLAVE} \
    -auto-reseed=${AUTO_RESEED} \
    -reseed-timeout=${RESEED_TIMEOUT} \
    -sandbox-disk-limit=${DISK_LIMIT} \
    -sandbox-cpu-limit=${CPU_LIMIT} \
    -sandbox-mem-limit=${MEM_LIMIT} \
    -admin-port=${PORT0} \
    -driver-port=${PORT1} \
    -artifact-port=${PORT2} \
    -framework-weburi=${WEBURI}"

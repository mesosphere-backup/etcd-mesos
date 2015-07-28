FROM busybox
ADD . /work
WORKDIR /work
CMD ls; pwd; env; \
bin/etcd_scheduler -alsologtostderr=true \
		-master="master:5050" \
		-task-count=3 \
		-zk-framework-persist="zk://zk:2181/etcd-mesos" \
		-v=2

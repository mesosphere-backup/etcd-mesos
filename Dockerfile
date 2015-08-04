FROM debian
ADD . /work
WORKDIR /work
CMD sh -c '/work/bin/etcd_scheduler --alsologtostderr=true \
    --master="master:5050" \
    -v=2'

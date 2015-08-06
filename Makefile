default: clean bin/etcd-mesos-executor bin/etcd-mesos-scheduler bin/etcd-mesos-proxy  bin/etcd

run: clean bin/etcd-mesos-executor bin/etcd run-scheduler

clean:
	-rm bin/etcd-*

bin:
	-mkdir bin

bin/etcd-mesos-scheduler: bin
	go build -o bin/etcd-mesos-scheduler cmd/etcd-mesos-scheduler/app.go

bin/etcd-mesos-executor: bin
	go build -o bin/etcd-mesos-executor cmd/etcd-mesos-executor/app.go

bin/etcd-mesos-proxy: bin
	go build -o bin/etcd-mesos-proxy cmd/etcd-mesos-proxy/app.go

bin/etcd: bin
	git submodule init
	git submodule update
	cd _vendor/coreos/etcd; ./build; mv bin/* ../../../bin/
  
run-scheduler:
	go run -race cmd/etcd-mesos-scheduler/app.go -logtostderr=true

run-scheduler-with-zk:
	go run -race cmd/etcd-mesos-scheduler/app.go -logtostderr=true \
		-master="zk://localhost:2181/mesos" \
		-cluster-name="t1" \
		-cluster-size=5 \
		-zk-framework-persist="zk://localhost:2181/etcd-mesos"

run-proxy:
	go run -race cmd/etcd-mesos-proxy/app.go \
		-master="zk://localhost:2181/mesos" \
		-cluster-name="t1"

install:
	go install ./cmd/...

cover:
	for i in `dirname **/*_test.go | grep -v "_vendor" | sort | uniq`; do \
		echo $$i; \
		go test -v -race ./$$i/... -coverprofile=em-coverage.out; \
		go tool cover -func=em-coverage.out; rm em-coverage.out; \
	done

test:
	go test -race ./...

docker:
	docker build -t etcd-mesos .

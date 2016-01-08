org_path="github.com/mesosphere"
repo_path="${org_path}/etcd-mesos"
mkfile_path	:= $(abspath $(lastword $(MAKEFILE_LIST)))
current_dir	:= $(patsubst %/,%,$(dir $(mkfile_path)))
GOPATH=${current_dir}/Godeps/_workspace
DOCKER_ORG=mesosphere
VERSION=0.1.1

default: clean deps build

clean:
	-rm bin/etcd-*

deps:
	rm -f ${GOPATH}/src/${repo_path}
	mkdir -p ${GOPATH}/src/${org_path}
	ln -s ${current_dir} ${GOPATH}/src/${repo_path}

build: bin/etcd-mesos-executor bin/etcd-mesos-scheduler bin/etcd-mesos-proxy bin/etcd

run: clean bin/etcd-mesos-executor bin/etcd run-scheduler

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
		-framework-name="etcd-t1" \
		-cluster-size=5 \
		-zk-framework-persist="zk://localhost:2181/etcd-mesos"

run-proxy:
	go run -race cmd/etcd-mesos-proxy/app.go \
		-master="zk://localhost:2181/mesos" \
		-framework-name="etcd-t1"

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

docker_build:
	docker run --rm -v "$$PWD":/go/src/github.com/mesosphere/etcd-mesos \
		-e GOPATH=/go \
		-w /go/src/github.com/mesosphere/etcd-mesos \
		golang:1.4.2 make

docker: docker_build
	docker build -t $(DOCKER_ORG)/etcd-mesos:$(VERSION) .

marathon: docker
	curl -X POST http://localhost:8080/v2/apps -d @marathon.json -H "Content-type: application/json"

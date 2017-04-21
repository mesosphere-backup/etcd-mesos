org_path="github.com/mesosphere"
repo_path="${org_path}/etcd-mesos"
mkfile_path	:= $(abspath $(lastword $(MAKEFILE_LIST)))
current_dir	:= $(patsubst %/,%,$(dir $(mkfile_path)))

# TODO document possible environment variables
DOCKER_ORG=mesosphere
VERSION=0.1.3
DOCKER_PUSH_ENABLED=0

default: clean build

.PHONY: clean
.SILENT: clean
clean:
	@rm -rf bin

.PHONY: format
.SILENT: format
format:
	echo "gofmt'ing..."
	@govendor fmt +local

.PHONY: bin
.SILENT: bin
bin:
	@mkdir -p bin

.PHONY: build
build: build_scheduler build_executor build_proxy build_etcd

.PHONY: build_scheduler
.SILENT: build_scheduler
build_scheduler: bin
	echo "Building etcd-mesos-scheduler.."
	go build -o bin/etcd-mesos-scheduler ./cmd/etcd-mesos-scheduler

.PHONY: build_executor
.SILENT: build_executor
build_executor: bin
	echo "Building etcd-mesos-executor.."
	go build -o bin/etcd-mesos-executor ./cmd/etcd-mesos-executor

.PHONY: build_proxy
.SILENT: build_proxy
build_proxy: bin
	echo "Building etcd-mesos-proxy.."
	go build -o bin/etcd-mesos-proxy ./cmd/etcd-mesos-proxy

.PHONY: build_etcd
.SILENT: build_etcd
build_etcd: bin
	echo "Building etcd binaries.."
	@git submodule init
	@git submodule update
	cd _vendor/coreos/etcd; ./build; mv bin/* ../../../bin/

.PHONY: run
run: format run_scheduler

# TODO add configurable Zookeeper IP and cluster size
.PHONY: run_scheduler
run_scheduler:
	go run -race ./cmd/etcd-mesos-scheduler/app.go -logtostderr=true \
		-master="zk://localhost:2181/mesos" \
		-framework-name="etcd-t1" \
		-cluster-size=5 \
		-zk-framework-persist="zk://localhost:2181/etcd-mesos"

# TODO add configurable Zookeeper IP
.PHONY: run_proxy
run_proxy:
	go run -race ./cmd/etcd-mesos-proxy/app.go \
		-master="zk://localhost:2181/mesos" \
		-framework-name="etcd-t1"

install:
	@govendor install +local

# TODO fix because doesn't work (at least on MacOS X)
cover:
	for i in `dirname **/*_test.go | grep -v "_vendor" | sort | uniq`; do \
		echo $$i; \
		go test -v -race ./$$i/... -coverprofile=em-coverage.out; \
		go tool cover -func=em-coverage.out; rm em-coverage.out; \
	done

test:
	@govendor test -race +local

.SILENT: docker_build
docker_build:
	docker run --rm -v "$$PWD":/go/src/github.com/mesosphere/etcd-mesos \
		-e GOPATH=/go \
		-w /go/src/github.com/mesosphere/etcd-mesos \
		golang:1.8.1 \
		make

.SILENT: docker
docker: docker_build
	docker build --no-cache -t $(DOCKER_ORG)/etcd-mesos:$(VERSION) .
	test ${DOCKER_PUSH_ENABLED} = 0 || docker push $(DOCKER_ORG)/etcd-mesos:$(VERSION)

# TODO add configurable Marathon IP
marathon: docker
	curl -X POST http://localhost:8080/v2/apps -d @marathon.json -H "Content-type: application/json"

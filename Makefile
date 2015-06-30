default: clean bin/etcd_executor bin/etcd_scheduler bin/etcd

run: clean bin/etcd_executor bin/etcd run-scheduler

clean:
	-rm bin/etcd_*

bin:
	-mkdir bin

bin/etcd_scheduler: bin
	go build -o bin/etcd_scheduler cmd/etcd-scheduler/app.go

bin/etcd_executor: bin
	go build -o bin/etcd_executor cmd/etcd-executor/app.go

bin/etcd: bin
	git submodule init
	git submodule update
	cd vendor/coreos/etcd; ./build; mv bin/* ../../../bin/
  
run-scheduler:
	go run -race cmd/etcd-scheduler/app.go -logtostderr=true

install:
	go install ./cmd/...

cover:
	go test -v -race ./scheduler/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v -race ./executor/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v -race ./rpc/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v -race ./offercache/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v -race ./config/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out

test:
	go test -race ./scheduler/...
	go test -race ./executor/...
	go test -race ./rpc/...
	go test -race ./offercache/...
	go test -race ./config/...

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
	go run cmd/etcd-scheduler/app.go -logtostderr=true

install:
	go install ./cmd/...

cover:
	go test -v ./scheduler/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v ./executor/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v ./rpc/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v ./offercache/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out
	go test -v ./config/... -coverprofile=em-coverage.out; go tool cover -func=em-coverage.out; rm em-coverage.out

test:
	go test ./scheduler/...
	go test ./executor/...
	go test ./rpc/...

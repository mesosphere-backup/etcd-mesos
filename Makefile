default: clean bin bin/etcd_executor bin/etcd_scheduler bin/etcd

run: clean bin/etcd_executor run-scheduler

clean:
	-rm bin/etcd_*

bin:
	-mkdir bin

bin/etcd_scheduler:
	go build -o bin/etcd_scheduler scheduler/main/main.go

bin/etcd_executor:
	go build -o bin/etcd_executor executor/main/main.go

bin/etcd:
	git submodule init
	git submodule update
	cd vendor/coreos/etcd; ./build; mv bin/* ../../../bin/
  
run-scheduler:
	go run scheduler/main/main.go -logtostderr=true

test:
	go test ./scheduler/...
	go test ./executor/...
	go test ./rpc/...

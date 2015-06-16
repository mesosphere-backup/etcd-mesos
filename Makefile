default: clean bin bin/etcd_executor bin/etcd run-scheduler

clean:
	-rm bin/etcd_*

bin:
	-mkdir bin

bin/etcd_executor:
	go build -o bin/etcd_executor executor/main/main.go

bin/etcd:
	git submodule init
	git submodule update
	cd vendor/coreos/etcd; ./build; mv bin/* ../../../bin/
  
run-scheduler:
	go run scheduler/main/main.go -logtostderr=true

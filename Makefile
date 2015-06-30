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
	for i in `dirname **/*_test.go | grep -v "vendor" | sort | uniq`; do \
		echo $$i; \
		go test -v -race ./$$i/... -coverprofile=em-coverage.out; \
		go tool cover -func=em-coverage.out; rm em-coverage.out; \
	done

test:
	for i in `dirname **/*_test.go | grep -v "vendor" | sort | uniq`; do \
		echo $$i; \
		go test -race ./$$i/... ;\
	done

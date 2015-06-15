// +build etcd-scheduler

/**
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/mesosphere/etcd-mesos/common"
	"github.com/mesosphere/etcd-mesos/offercache"
	"github.com/mesosphere/etcd-mesos/rpc"

	"github.com/gogo/protobuf/proto"
	log "github.com/golang/glog"
	"github.com/mesos/mesos-go/auth"
	"github.com/mesos/mesos-go/auth/sasl"
	"github.com/mesos/mesos-go/auth/sasl/mech"
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/mesos/mesos-go/scheduler"
	"github.com/samuel/go-zookeeper/zk"
	"golang.org/x/net/context"
)

const (
	CPUS_PER_TASK            = 1
	MEM_PER_TASK             = 256
	DISK_PER_TASK            = 1024
	PORTS_PER_TASK           = 2
	ETCD_INVOCATION_TEMPLATE = `./etcd --data-dir="etcd_data"
		--name="{{.Name}}"
		--initial-cluster-state="{{.Type}}"
		--listen-peer-urls="http://{{.Host}}:{{.RpcPort}}"
		--initial-advertise-peer-urls="http://{{.Host}}:{{.RpcPort}}"
		--listen-client-urls="http://{{.Host}}:{{.ClientPort}}"
		--advertise-client-urls="http://{{.Host}}:{{.ClientPort}}"
		--initial-cluster="{{.Cluster}}"
	`
)

type State int32

const (
	Initializing State = iota
	Restoring
	Monitoring
	Healing
	Exiting
)

type EtcdScheduler struct {
	mut                    sync.RWMutex
	state                  State
	executorUris           []*mesos.CommandInfo_URI
	highestInstanceID      int64
	running                map[string]*common.EtcdConfig
	offerCache             *offercache.OfferCache
	launchChan             chan struct{}
	pauseChan              chan struct{}
	restorePath            string
	master                 string
	executorPath           string
	etcdPath               string
	clusterName            string
	zkConnect              string
	zkChroot               string
	zkServers              []string
	taskCount              int
	singleInstancePerSlave bool
	mesosAuthPrincipal     string
	mesosAuthSecretFile    string
	address                string
	artifactPort           int
	authProvider           string
}

type EtcdParams struct {
	common.EtcdConfig
	Cluster string
}

type OfferResources struct {
	cpus  float64
	mems  float64
	disk  float64
	ports []*mesos.Value_Range
}

func newEtcdScheduler(executorUris []*mesos.CommandInfo_URI) *EtcdScheduler {
	return &EtcdScheduler{
		state:             Initializing,
		highestInstanceID: time.Now().Unix(),
		executorUris:      executorUris,
		running:           make(map[string]*common.EtcdConfig),
		zkServers:         []string{},
	}
}

// ----------------------- mesos callbacks ------------------------- //

func (s *EtcdScheduler) Registered(
	driver scheduler.SchedulerDriver,
	frameworkId *mesos.FrameworkID,
	masterInfo *mesos.MasterInfo,
) {
	// Pump the brakes to allow some time for reconciliation.
	s.pauseChan <- struct{}{}
	s.pauseChan <- struct{}{}
	if s.zkConnect != "" {
		err := persistFrameworkID(frameworkId, s.zkServers, s.zkChroot, s.clusterName)
		if err != nil && err != zk.ErrNodeExists {
			log.Fatalf("Failed to persist framework ID: %s", err)
		} else if err == zk.ErrNodeExists {
			log.Warning("Framework ID is already persisted for this cluster.")
		}
	}
	log.Infoln("Framework Registered with Master ", masterInfo)
	_, err := driver.ReconcileTasks([]*mesos.TaskStatus{})
	if err != nil {
		log.Errorf("Error while calling ReconcileTasks: %s", err)
	}
}

func (s *EtcdScheduler) Reregistered(
	driver scheduler.SchedulerDriver,
	masterInfo *mesos.MasterInfo,
) {
	// Pump the brakes to allow some time for reconciliation.
	s.pauseChan <- struct{}{}
	s.pauseChan <- struct{}{}
	// TODO(tyler) check invariant: current persisted fwid in zk should be the same as this one
	log.Infoln("Framework Re-Registered with Master ", masterInfo)
	_, err := driver.ReconcileTasks([]*mesos.TaskStatus{})
	if err != nil {
		log.Errorf("Error while calling ReconcileTasks: %s", err)
	}
}

func (s *EtcdScheduler) Disconnected(scheduler.SchedulerDriver) {
	// TODO(tyler) disable all external actions
}

func (s *EtcdScheduler) ResourceOffers(
	driver scheduler.SchedulerDriver,
	offers []*mesos.Offer,
) {
	for _, offer := range offers {
		resources := parseOffer(offer)

		totalPorts := uint64(0)
		for _, pr := range resources.ports {
			totalPorts += (*pr.End + 1) - *pr.Begin
		}

		alreadyUsingSlave := false
		s.mut.RLock()
		for _, config := range s.running {
			if config.SlaveID == offer.GetSlaveId().GetValue() {
				alreadyUsingSlave = true
				break
			}
		}
		s.mut.RUnlock()
		if alreadyUsingSlave {
			log.Infoln("Already using this slave for etcd instance.")
			if s.singleInstancePerSlave {
				log.Infoln("Skipping offer.")
				continue
			}
			log.Infoln("-single-instance-per-slave is false, continuing.")
		}

		log.Infoln("Received Offer <", offer.Id.GetValue(),
			"> with cpus=", resources.cpus,
			" mem=", resources.mems,
			" ports=", totalPorts,
			" disk=", resources.disk,
			" from slave ", *offer.SlaveId.Value)

		if resources.cpus >= CPUS_PER_TASK &&
			resources.mems >= MEM_PER_TASK &&
			totalPorts >= PORTS_PER_TASK &&
			resources.disk >= DISK_PER_TASK &&
			s.offerCache.Push(offer) {
			log.Infoln("Adding offer to offer cache.")
			s.launchChan <- struct{}{}
		} else {
			log.Infoln("Offer rejected.")
		}
	}
}

func (s *EtcdScheduler) StatusUpdate(
	driver scheduler.SchedulerDriver,
	status *mesos.TaskStatus,
) {
	log.Infoln(
		"Status update: task",
		status.TaskId.GetValue(),
		" is in state ",
		status.State.Enum().String(),
	)

	s.mut.Lock()
	defer s.mut.Unlock()

	switch status.GetState() {
	case mesos.TaskState_TASK_LOST,
		mesos.TaskState_TASK_FINISHED,
		mesos.TaskState_TASK_KILLED,
		mesos.TaskState_TASK_ERROR,
		mesos.TaskState_TASK_FAILED:
		delete(s.running, status.TaskId.GetValue())
		go func() {
			rpc.RemoveInstance(s.running, status.GetTaskId().GetValue())
			// Allow some time for out-of-quorum followers to hopefully sync the change.
			// TODO(tyler) is this necessary?
			time.Sleep(3 * time.Second)
			s.launchChan <- struct{}{}
		}()
	case mesos.TaskState_TASK_RUNNING:
		etcdConfig := common.EtcdConfig{}
		err := json.Unmarshal([]byte(status.GetTaskId().GetValue()), &etcdConfig)
		if err != nil {
			log.Errorf(
				"Could not deserialize taskid into EtcdConfig: %s",
				err,
			)
			return
			// TODO(tyler) kill invalid task? does data get set to nothing sometimes? can a state enter running twice?
		} else {
			s.running[etcdConfig.Name] = &etcdConfig
		}

		// During reconcilliation, we may find nodes with higher ID's due to ntp drift
		etcdIndexParts := strings.Split(etcdConfig.Name, "-")
		if len(etcdIndexParts) != 2 {
			log.Warning("Task has a Name that does not follow the form etcd-<index>")
		} else {
			etcdIndex, err := strconv.ParseInt(etcdIndexParts[1], 10, 64)
			if err != nil {
				log.Warning("Task has a Name that does not follow the form etcd-<index>")
			} else {
				if etcdIndex > s.highestInstanceID {
					s.highestInstanceID = etcdIndex + 1
				}
			}
		}

		_, present := s.running[etcdConfig.Name]
		if !present {
			// TODO(tyler) what other reconciliation logic do we need to do for housekeeping?
			// TODO(tyler) pull EtcdConfig json out of status.GetData() and unmarshal it.
			s.running[etcdConfig.Name] = &etcdConfig
		}

		if len(s.running) < s.taskCount {
			s.state = Healing
		} else {
			s.state = Monitoring
		}
	default:
		log.Warningf("Received unhandled task state: %+v", status.GetState())
	}

	if len(s.running) == 0 {
		// TODO logic for restoring from backup
		s.state = Initializing
	}
}

func (s *EtcdScheduler) OfferRescinded(
	driver scheduler.SchedulerDriver,
	offerId *mesos.OfferID,
) {
	log.Info("received OfferRescinded rpc")
	s.offerCache.Rescind(offerId)
}

func (s *EtcdScheduler) FrameworkMessage(
	driver scheduler.SchedulerDriver,
	exec *mesos.ExecutorID,
	slave *mesos.SlaveID,
	msg string,
) {
	log.Info("received framework message: %s", msg)
}
func (s *EtcdScheduler) SlaveLost(
	scheduler.SchedulerDriver,
	*mesos.SlaveID,
) {
	log.Info("received slave lost rpc")
}
func (s *EtcdScheduler) ExecutorLost(
	scheduler.SchedulerDriver,
	*mesos.ExecutorID,
	*mesos.SlaveID,
	int,
) {
	log.Info("received executor lost rpc")
}

func (s *EtcdScheduler) Error(driver scheduler.SchedulerDriver, err string) {
	log.Infoln("Scheduler received error:", err)
	if err == "Completed framework attempted to re-register" {
		// TODO(tyler) automatically restart, don't expect this to be restarted externally
		clearZKState(s.zkServers, s.zkChroot, s.clusterName)
		log.Fatalf("Removing reference to completed framework in zookeeper and dying.")
	}
}

// ----------------------- helper functions ------------------------- //

// SerialLauncher performs the launching of all tasks in a time-limited
// way.  This helps to prevent misconfiguration by allowing time for state
// to propagate.
func (s *EtcdScheduler) SerialLauncher(driver scheduler.SchedulerDriver) {
	for {
		// pauseChan needs priority over launchChan, so we need to try it
		// before randomly selecting from either of them below.
		for {
			select {
			case <-s.pauseChan:
				log.Info("SerialLauncher sleeping for 10 seconds.")
				time.Sleep(10 * time.Second)
			default:
				goto FCFSPauseOrLaunch
			}
		}
	FCFSPauseOrLaunch:
		select {
		case _, ok := <-s.launchChan:
			if !ok {
				return
			}
			s.launchOne(driver)

			// Wait 10 seconds between launches to allow a cluster to settle.
			time.Sleep(10 * time.Second)
		case <-s.pauseChan:
			log.Info("SerialLauncher sleeping for 10 seconds.")
			time.Sleep(10 * time.Second)
		}
	}
}

func (s *EtcdScheduler) launchOne(driver scheduler.SchedulerDriver) {
	log.Infoln("Attempting to launch a task.")
	s.mut.RLock()
	nrunning := len(s.running)
	s.mut.RUnlock()
	log.Infoln("nrunning: ", nrunning, " taskCount: ", s.taskCount)
	log.Infof("running: %+v", s.running)
	if nrunning >= s.taskCount {
		log.Infoln("Already running enough tasks.")
		return
	}

	offer := s.offerCache.BlockingPop()
	s.mut.Lock()
	defer s.mut.Unlock()
	for _, etcdConfig := range s.running {
		if etcdConfig.SlaveID == offer.SlaveId.GetValue() {
			log.Infoln("Already running an etcd instance on this slave.")
			// TODO(tyler) should we be dropping this offer here or requeueing it?
			if s.singleInstancePerSlave {
				return
			}
			log.Infoln("Launching anyway due to -single-instance-per-slave " +
				"argument of false.")
		}
	}

	nrunning = len(s.running)
	if nrunning >= s.taskCount {
		s.offerCache.Push(offer)
		log.Infoln("Already running enough tasks.")
		return
	}
	resources := parseOffer(offer)

	// TODO(tyler) this is a broken hack
	lowest := *resources.ports[0].Begin
	rpcPort := lowest
	clientPort := lowest + 1

	s.highestInstanceID++

	var clusterType string
	if s.state == Initializing {
		clusterType = "new"
	} else {
		clusterType = "existing"
	}

	name := "etcd-" + strconv.FormatInt(s.highestInstanceID, 10)
	instance := &common.EtcdConfig{
		Name:       name,
		Host:       *offer.Hostname,
		RpcPort:    rpcPort,
		ClientPort: clientPort,
		Type:       clusterType,
		SlaveID:    offer.GetSlaveId().GetValue(),
	}
	running := []*common.EtcdConfig{instance}
	for _, r := range s.running {
		running = append(running, r)
	}
	config := formatConfig(instance, running)

	serializedConfig, err := json.Marshal(instance)
	if err != nil {
		log.Errorf("Could not serialize our new task!")
		return
	}

	stringSerializedConfig := string(serializedConfig)
	// TODO(tyler) is there a better place to put this so that it will persist in all StatusUpdates?
	taskId := &mesos.TaskID{
		Value: &stringSerializedConfig,
	}

	executor := s.prepareExecutorInfo(instance, s.executorUris, config)
	task := &mesos.TaskInfo{
		Name:     proto.String(name),
		TaskId:   taskId,
		SlaveId:  offer.SlaveId,
		Executor: executor,
		Resources: []*mesos.Resource{
			util.NewScalarResource("cpus", CPUS_PER_TASK),
			util.NewScalarResource("mem", MEM_PER_TASK),
			util.NewScalarResource("disk", DISK_PER_TASK),
			util.NewRangesResource("ports", []*mesos.Value_Range{
				util.NewValueRange(uint64(rpcPort), uint64(clientPort)),
			}),
		},
	}

	// TODO(tyler) put this in pending, not running.  would also need rate limit or something.
	s.running[instance.Name] = instance

	log.Infof(
		"Prepared task: %s with offer %s for launch\n",
		task.GetName(),
		offer.Id.GetValue(),
	)
	log.Infof("Launching etcd instance with command: %s", config)

	tasks := []*mesos.TaskInfo{task}
	log.Infoln("Launching ", len(tasks), "tasks for offer", offer.Id.GetValue())
	// TODO(tyler) move configuration to executor
	go rpc.ConfigureInstance(s.running, instance.Name)
	// TODO(tyler) persist failover state (pending task)

	driver.LaunchTasks(
		[]*mesos.OfferID{offer.Id},
		tasks,
		&mesos.Filters{
			RefuseSeconds: proto.Float64(1),
		},
	)
}
func parseOffer(offer *mesos.Offer) OfferResources {
	getResources := func(resourceName string) []*mesos.Resource {
		return util.FilterResources(
			offer.Resources,
			func(res *mesos.Resource) bool {
				return res.GetName() == resourceName
			},
		)
	}

	cpuResources := getResources("cpus")
	cpus := 0.0
	for _, res := range cpuResources {
		cpus += res.GetScalar().GetValue()
	}

	memResources := getResources("mem")
	mems := 0.0
	for _, res := range memResources {
		mems += res.GetScalar().GetValue()
	}

	portResources := getResources("ports")
	ports := make([]*mesos.Value_Range, 0, 10)
	for _, res := range portResources {
		ranges := res.GetRanges()
		ports = append(ports, ranges.GetRange()...)
	}

	diskResources := getResources("disk")
	disk := 0.0
	for _, res := range diskResources {
		disk += res.GetScalar().GetValue()
	}

	return OfferResources{
		cpus:  cpus,
		mems:  mems,
		disk:  disk,
		ports: ports,
	}
}

func serveExecutorArtifact(path, address string, artifactPort int) *string {
	serveFile := func(pattern string, filename string) {
		http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filename)
		})
	}

	// Create base path (http://foobar:5000/<base>)
	pathSplit := strings.Split(path, "/")
	var base string
	if len(pathSplit) > 0 {
		base = pathSplit[len(pathSplit)-1]
	} else {
		base = path
	}
	serveFile("/"+base, path)

	hostURI := fmt.Sprintf("http://%s:%d/%s", address, artifactPort, base)
	log.V(2).Infof("Hosting artifact '%s' at '%s'", path, hostURI)

	return &hostURI
}

func (s *EtcdScheduler) prepareExecutorInfo(instance *common.EtcdConfig,
	executorUris []*mesos.CommandInfo_URI,
	etcdExec string) *mesos.ExecutorInfo {

	_, executorBin := filepath.Split(s.executorPath)
	executorCommand := fmt.Sprintf("./%s -exec=\"%s\" -log_dir=./",
		executorBin,
		etcdExec)

	// Create mesos scheduler driver.
	return &mesos.ExecutorInfo{
		ExecutorId: util.NewExecutorID(instance.Name),
		Name:       proto.String("etcd"),
		Source:     proto.String("go_test"),
		Command: &mesos.CommandInfo{
			Value: proto.String(executorCommand),
			Uris:  executorUris,
		},
		Resources: []*mesos.Resource{
			util.NewScalarResource("cpus", 0.1),
			util.NewScalarResource("mem", 32),
		},
	}
}

func parseIP(address string) net.IP {
	addr, err := net.LookupIP(address)
	if err != nil {
		log.Fatal(err)
	}
	if len(addr) < 1 {
		log.Fatalf("failed to parse IP from address '%v'", address)
	}
	return addr[0]
}

func formatConfig(
	newServer *common.EtcdConfig,
	existingServers []*common.EtcdConfig,
) string {
	formatted := make([]string, 0, len(existingServers))
	for _, e := range existingServers {
		formatted = append(formatted,
			fmt.Sprintf("%s=http://%s:%d", e.Name, e.Host, e.RpcPort))
	}

	params := EtcdParams{EtcdConfig: *newServer}
	params.Cluster = strings.Join(formatted, ",")

	var config bytes.Buffer
	t := template.Must(template.New("name").Parse(ETCD_INVOCATION_TEMPLATE))
	err := t.Execute(&config, params)
	if err != nil {
		log.Error(err)
	}

	return strings.Replace(config.String(), "\n", " ", -1)
}

func parseZKURI(zkURI string) (servers []string, chroot string, err error) {
	servers = []string{}

	// this is to use the canonical zk://host1:ip,host2/zkChroot format
	strippedZKConnect := strings.TrimLeft(zkURI, "zk://")
	parts := strings.Split(strippedZKConnect, "/")
	if len(parts) == 2 {
		if parts[1] == "" {
			return nil, "", errors.New("ZK chroot must not be the root path \"/\"!")
		}
		chroot = "/" + parts[1]
		servers = strings.Split(parts[0], ",")
	} else if len(parts) == 1 {
		servers = strings.Split(parts[0], ",")
	} else {
		return nil, "", errors.New("ZK URI must be of the form " +
			"zk://$host1:$port1,$host2:$port2/path/to/zk/chroot")
	}
	for _, zk := range servers {
		if len(strings.Split(zk, ":")) != 2 {
			return nil, "", errors.New("ZK URI must be of the form " +
				"zk://$host1:$port1,$host2:$port2/path/to/zk/chroot")
		}
	}
	return servers, chroot, nil
}

func persistFrameworkID(fwid *mesos.FrameworkID, zkServers []string, zkChroot string, clusterName string) error {
	c, _, err := zk.Connect(zkServers, time.Second*5)
	if err != nil {
		return err
	}
	defer c.Close()
	// attempt to create the path
	_, err = c.Create(
		zkChroot,
		[]byte(""),
		0,
		zk.WorldACL(zk.PermAll),
	)
	if err != nil && err != zk.ErrNodeExists {
		return err
	}
	// attempt to write framework ID to <path> / <clusterName>
	_, err = c.Create(zkChroot+"/"+clusterName,
		[]byte(fwid.GetValue()),
		0,
		zk.WorldACL(zk.PermAll))
	// TODO(tyler) when err is zk.ErrNodeExists, cross-check value
	if err != nil {
		return err
	}
	log.Info("Successfully persisted Framework ID to zookeeper.")

	return nil
}

func getPreviousFrameworkID(zkServers []string, zkChroot string, clusterName string) (string, error) {
	c, _, err := zk.Connect(zkServers, time.Second*5)
	if err != nil {
		return "", err
	}
	defer c.Close()
	rawData, _, err := c.Get(zkChroot + "/" + clusterName)
	return string(rawData), err
}

// TODO(tyler) make this more testable.
func clearZKState(zkServers []string, zkChroot string, clusterName string) error {
	c, _, err := zk.Connect(zkServers, time.Second*5)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Delete(zkChroot+"/"+clusterName, -1)
}

// ----------------------- entry point ------------------------- //

func main() {
	executorPath := flag.String("executor", "./bin/etcd_executor", "Path to test executor")
	etcdPath := flag.String("etcd", "./bin/etcd", "Path to test executor")
	address := flag.String("address", "127.0.0.1", "Binding address for artifact server")
	artifactPort := flag.Int("artifactPort", 12345, "Binding port for artifact server") // TODO(tyler) require this to be passed in
	flag.Parse()

	executorUris := []*mesos.CommandInfo_URI{}
	execUri := serveExecutorArtifact(*executorPath, *address, *artifactPort)
	executorUris = append(executorUris, &mesos.CommandInfo_URI{
		Value:      execUri,
		Executable: proto.Bool(true),
	})
	etcdUri := serveExecutorArtifact(*etcdPath, *address, *artifactPort)
	executorUris = append(executorUris, &mesos.CommandInfo_URI{
		Value:      etcdUri,
		Executable: proto.Bool(true),
	})

	go http.ListenAndServe(fmt.Sprintf("%s:%d", *address, *artifactPort), nil)
	log.V(2).Info("Serving executor artifacts...")

	bindingAddress := parseIP(*address)

	etcdScheduler := newEtcdScheduler(executorUris)
	etcdScheduler.executorPath = *executorPath
	etcdScheduler.address = *address
	etcdScheduler.artifactPort = *artifactPort

	flag.StringVar(&etcdScheduler.restorePath, "restore", "", "Local path or URI for an etcd backup to restore as a new cluster.")
	flag.StringVar(&etcdScheduler.master, "master", "127.0.0.1:5050", "Master address <ip:port>")
	flag.StringVar(&etcdScheduler.clusterName, "clusterName", "default", "Unique name of this etcd cluster")
	flag.IntVar(&etcdScheduler.taskCount, "task-count", 5, "Total task count to run.")
	flag.BoolVar(&etcdScheduler.singleInstancePerSlave, "single-instance-per-slave", true, "Only allow one etcd instance to be started per slave.")
	flag.StringVar(&etcdScheduler.mesosAuthPrincipal, "mesos_authentication_principal", "", "Mesos authentication principal.")
	flag.StringVar(&etcdScheduler.mesosAuthSecretFile, "mesos_authentication_secret_file", "", "Mesos authentication secret file.")
	flag.StringVar(&etcdScheduler.authProvider, "mesos_authentication_provider", sasl.ProviderName,
		fmt.Sprintf("Authentication provider to use, default is SASL that supports mechanisms: %+v", mech.ListSupported()))

	zkConnect := flag.String("zk", "", "zookeeper URI")
	flag.Parse()
	fwinfo := &mesos.FrameworkInfo{
		User:            proto.String(""), // Mesos-go will fill in user.
		Name:            proto.String("etcd: " + etcdScheduler.clusterName),
		FailoverTimeout: proto.Float64(60), // TODO(tyler) increase this
		// TODO(tyler) Role: proto.String("etcd_scheduler"),
	}

	cred := (*mesos.Credential)(nil)
	if etcdScheduler.mesosAuthPrincipal != "" {
		fwinfo.Principal = proto.String(etcdScheduler.mesosAuthPrincipal)
		secret, err := ioutil.ReadFile(etcdScheduler.mesosAuthSecretFile)
		if err != nil {
			log.Fatal(err)
		}
		cred = &mesos.Credential{
			Principal: proto.String(etcdScheduler.mesosAuthPrincipal),
			Secret:    secret,
		}
	}

	etcdScheduler.offerCache = offercache.NewOfferCache(etcdScheduler.taskCount)
	etcdScheduler.launchChan = make(chan struct{}, etcdScheduler.taskCount*2048)
	etcdScheduler.pauseChan = make(chan struct{}, etcdScheduler.taskCount*2048)
	zkServers, zkChroot, err := parseZKURI(*zkConnect)
	etcdScheduler.zkServers = zkServers
	etcdScheduler.zkChroot = zkChroot
	if err != nil && *zkConnect != "" {
		log.Fatalf("Error parsing zookeeper URI of %s: %s", *zkConnect, err)
	} else if *zkConnect != "" {
		previous, err := getPreviousFrameworkID(zkServers, zkChroot, etcdScheduler.clusterName)
		if err != nil && err != zk.ErrNoNode {
			log.Fatalf("Could not retrieve previous framework ID: %s", err)
		} else if err == zk.ErrNoNode {
			log.Info("No previous persisted framework ID exists in zookeeper.")
		} else {
			log.Infof("Found stored framework ID, attempting to re-use: %s", previous)
			fwinfo.Id = &mesos.FrameworkID{
				Value: proto.String(previous),
			}
		}
	}

	config := scheduler.DriverConfig{
		Scheduler:      etcdScheduler,
		Framework:      fwinfo,
		Master:         etcdScheduler.master,
		Credential:     cred,
		BindingAddress: bindingAddress,
		WithAuthContext: func(ctx context.Context) context.Context {
			ctx = auth.WithLoginProvider(ctx, etcdScheduler.authProvider)
			ctx = sasl.WithBindingAddress(ctx, bindingAddress)
			return ctx
		},
	}

	driver, err := scheduler.NewMesosSchedulerDriver(config)

	if err != nil {
		log.Errorln("Unable to create a SchedulerDriver ", err.Error())
	}

	go etcdScheduler.SerialLauncher(driver)

	if stat, err := driver.Run(); err != nil {
		log.Infof("Framework stopped with status %s and error: %s\n",
			stat.String(),
			err.Error())
	}
}

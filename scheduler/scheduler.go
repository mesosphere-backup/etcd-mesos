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

package scheduler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gogo/protobuf/proto"
	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/mesos/mesos-go/scheduler"
	"github.com/samuel/go-zookeeper/zk"

	"github.com/mesosphere/etcd-mesos/common"
	"github.com/mesosphere/etcd-mesos/offercache"
	"github.com/mesosphere/etcd-mesos/rpc"
)

const (
	cpusPerTask            = 1
	memPerTask             = 256
	diskPerTask            = 1024
	portsPerTask           = 2
	etcdInvocationTemplate = `./etcd --data-dir="etcd_data"
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
	// The scheduler is Starting when it is adding the first node, optionally
	// from a backup.
	Starting State = iota
	// The scheduler is Growing when it is adding new nodes.
	Growing
	// The scheduler is Restoring when it is restoring an etcd snapshot from
	// S3, HDFS, or the local system.
	Restoring
	// The scheduler is SteadyState when it is in steady state, running N nodes.
	SteadyState
	// The scheduler is Pruning when it is deconfiguring dead etcd instances.
	Pruning
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
	RestorePath            string
	Master                 string
	ExecutorPath           string
	EtcdPath               string
	ClusterName            string
	ZkConnect              string
	ZkChroot               string
	ZkServers              []string
	taskCount              int
	SingleInstancePerSlave bool
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

func NewEtcdScheduler(
	taskCount int,
	executorUris []*mesos.CommandInfo_URI,
) *EtcdScheduler {
	return &EtcdScheduler{
		state:             Growing,
		highestInstanceID: time.Now().Unix(),
		executorUris:      executorUris,
		running:           make(map[string]*common.EtcdConfig),
		ZkServers:         []string{},
		taskCount:         taskCount,
		launchChan:        make(chan struct{}, 2048),
		pauseChan:         make(chan struct{}, 2048),
		offerCache:        offercache.NewOfferCache(taskCount),
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
	if s.ZkConnect != "" {
		err := rpc.PersistFrameworkID(
			frameworkId,
			s.ZkServers,
			s.ZkChroot,
			s.ClusterName,
		)
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
			if s.SingleInstancePerSlave {
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

		if resources.cpus >= cpusPerTask &&
			resources.mems >= memPerTask &&
			totalPorts >= portsPerTask &&
			resources.disk >= diskPerTask &&
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
		etcdConfig := common.EtcdConfig{}
		// Pump the brakes so that we have time to deconfigure the lost node
		// before adding a new one.  If we don't deconfigure first, we risk
		// split brain.
		s.pauseChan <- struct{}{}
		err := json.Unmarshal([]byte(status.GetTaskId().GetValue()), &etcdConfig)
		if err != nil {
			log.Errorf("Could not deserialize taskid into EtcdConfig: %s", err)
			break
		}
		delete(s.running, etcdConfig.Name)
		go func() {
			rpc.RemoveInstance(s.running, etcdConfig.Name)
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
			s.state = Pruning
		} else {
			s.state = SteadyState
		}
	default:
		log.Warningf("Received unhandled task state: %+v", status.GetState())
	}

	if len(s.running) == 0 {
		// TODO logic for restoring from backup
		s.state = Growing
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
		rpc.ClearZKState(s.ZkServers, s.ZkChroot, s.ClusterName)
		log.Fatalf(
			"Removing reference to completed " +
				"framework in zookeeper and dying.",
		)
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
				log.Info("SerialLauncher sleeping for 10 seconds " +
					"after receiving pause signal.")
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
			log.Info("SerialLauncher sleeping for 10 seconds after launch attempt.")
			time.Sleep(10 * time.Second)
		case <-s.pauseChan:
			log.Info("SerialLauncher sleeping for 10 seconds " +
				"after receiving pause signal.")
			time.Sleep(10 * time.Second)
		}
	}
}

func (s *EtcdScheduler) launchOne(driver scheduler.SchedulerDriver) {
	// TODO(tyler) NEVER launch a task when we have dead nodes in the member list
	log.Infoln("Attempting to launch a task.")
	s.mut.RLock()
	nrunning := len(s.running)
	s.mut.RUnlock()
	log.Infof(
		"running instances: %d desired: %d offers: %d",
		nrunning, s.taskCount, s.offerCache.Len(),
	)
	log.Infof("running: %+v", s.running)
	if nrunning >= s.taskCount {
		log.Infoln("Already running enough tasks.")
		return
	}

	members, err := rpc.MemberList(s.running)
	if err != nil {
		log.Errorf("Failed to retrieve running member list, "+
			"not launching a new task: %s", err)
		return
	}
	if len(members) == s.taskCount {
		log.Errorf("Cluster is already configured for desired number of nodes.  " +
			"Must deconfigure any dead nodes first or we may risk livelock.")
		return
	}
	err = rpc.HealthCheck(s.running)
	if err != nil && len(s.running) != 0 {
		log.Errorf("Failed health check, not launching a new task: %s", err)
		return
	}

	// Issue BlockingPop until we get back an offer we can use.
	var offer *mesos.Offer
	for {
		innerOffer, err := func() (*mesos.Offer, error) {
			offer := s.offerCache.BlockingPop()
			s.mut.RLock()
			defer s.mut.RUnlock()
			for _, etcdConfig := range s.running {
				if etcdConfig.SlaveID == offer.SlaveId.GetValue() {
					log.Infoln("Already running an etcd instance on this slave.")
					if s.SingleInstancePerSlave {
						return nil, errors.New("Already running on slave.")
					}
					log.Infoln("Launching anyway due to -single-instance-per-slave " +
						"argument of false.")
				}
			}
			return offer, nil
		}()
		if err == nil {
			offer = innerOffer
			break
		}
	}

	nrunning = len(s.running)
	if nrunning >= s.taskCount {
		log.Infoln("Already running enough tasks.")
		s.offerCache.Push(offer)
		return
	}
	resources := parseOffer(offer)

	// TODO(tyler) this is a broken hack
	lowest := *resources.ports[0].Begin
	rpcPort := lowest
	clientPort := lowest + 1

	s.highestInstanceID++

	var clusterType string
	if s.state == Growing {
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
		s.offerCache.Push(offer)
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
			util.NewScalarResource("cpus", cpusPerTask),
			util.NewScalarResource("mem", memPerTask),
			util.NewScalarResource("disk", diskPerTask),
			util.NewRangesResource("ports", []*mesos.Value_Range{
				util.NewValueRange(uint64(rpcPort), uint64(clientPort)),
			}),
		},
	}

	log.Infof(
		"Prepared task: %s with offer %s for launch\n",
		task.GetName(),
		offer.Id.GetValue(),
	)
	log.Infof("Launching etcd instance with command: %s", config)

	tasks := []*mesos.TaskInfo{task}
	log.Infoln("Launching ", len(tasks), "tasks for offer", offer.Id.GetValue())
	// TODO(tyler) move configuration to executor
	go rpc.ConfigureInstance(s.running, instance)
	// TODO(tyler) persist failover state (pending task)

	driver.LaunchTasks(
		[]*mesos.OfferID{offer.Id},
		tasks,
		&mesos.Filters{
			RefuseSeconds: proto.Float64(1),
		},
	)
	return
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

func ServeExecutorArtifact(path, address string, artifactPort int) *string {
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

	_, executorBin := filepath.Split(s.ExecutorPath)
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
	t := template.Must(template.New("name").Parse(etcdInvocationTemplate))
	err := t.Execute(&config, params)
	if err != nil {
		log.Error(err)
	}

	return strings.Replace(config.String(), "\n", " ", -1)
}

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
	"math"
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
	// The scheduler is Mutable during:
	// * starting up for the first time
	// * growing (recovering) from 1 to N nodes
	// * pruning dead nodes
	// * exiting
	Mutable State = iota
	// The scheduler is Immutable during:
	// * waiting for state to settle during initialization
	// * disconnection from the Mesos master
	// * performing a backup with the intention of seeding a new cluster
	Immutable
)

type EtcdScheduler struct {
	mut                    sync.RWMutex
	state                  State
	pending                map[string]struct{}
	running                map[string]*common.EtcdConfig
	highestInstanceID      int64
	executorUris           []*mesos.CommandInfo_URI
	offerCache             *offercache.OfferCache
	launchChan             chan struct{}
	pauseChan              chan struct{}
	chillFactor            time.Duration
	RestorePath            string
	Master                 string
	ExecutorPath           string
	EtcdPath               string
	ClusterName            string
	ZkConnect              string
	ZkChroot               string
	ZkServers              []string
	desiredInstanceCount   int
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
	desiredInstanceCount int,
	chillFactor int,
	executorUris []*mesos.CommandInfo_URI,
) *EtcdScheduler {
	return &EtcdScheduler{
		state:                Immutable,
		running:              map[string]*common.EtcdConfig{},
		pending:              map[string]struct{}{},
		highestInstanceID:    time.Now().Unix(),
		executorUris:         executorUris,
		ZkServers:            []string{},
		chillFactor:          time.Duration(chillFactor),
		desiredInstanceCount: desiredInstanceCount,
		launchChan:           make(chan struct{}, 2048),
		pauseChan:            make(chan struct{}, 2048),
		offerCache:           offercache.NewOfferCache(desiredInstanceCount),
	}
}

// ----------------------- mesos callbacks ------------------------- //

func (s *EtcdScheduler) Registered(
	driver scheduler.SchedulerDriver,
	frameworkId *mesos.FrameworkID,
	masterInfo *mesos.MasterInfo,
) {
	log.Infoln("Framework Registered with Master ", masterInfo)

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
	s.Initialize(driver)
}

func (s *EtcdScheduler) Reregistered(
	driver scheduler.SchedulerDriver,
	masterInfo *mesos.MasterInfo,
) {
	log.Infoln("Framework Reregistered with Master ", masterInfo)

	s.Initialize(driver)
}

func (s *EtcdScheduler) Disconnected(scheduler.SchedulerDriver) {
	s.mut.Lock()
	s.state = Immutable
	s.mut.Unlock()
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

		if resources.cpus < cpusPerTask {
			log.Infoln("Offer cpu is insufficient.")
		}

		if resources.mems < memPerTask {
			log.Infoln("Offer memory is insufficient.")
		}

		if totalPorts < portsPerTask {
			log.Infoln("Offer ports are insuffient.")
		}

		if resources.disk < diskPerTask {
			log.Infoln("Offer disk is insufficient.")
		}

		if resources.cpus >= cpusPerTask &&
			resources.mems >= memPerTask &&
			totalPorts >= portsPerTask &&
			resources.disk >= diskPerTask &&
			s.offerCache.Push(offer) {
			log.Infoln("Adding offer to offer cache.")
			s.QueueLaunchAttempt()
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

	log.Info("statusupdate attempt")
	s.mut.Lock()
	log.Info("statusupdate locked")
	defer s.mut.Unlock()
	defer log.Info("statusupdate unlocked")

	etcdConfig := common.EtcdConfig{}
	err := json.Unmarshal([]byte(status.GetTaskId().GetValue()), &etcdConfig)
	if err != nil {
		log.Errorf("!!!! Could not deserialize taskid into EtcdConfig: %s", err)
		return
	}

	// If this task was pending, we now know it's running or dead.
	delete(s.pending, etcdConfig.Name)

	switch status.GetState() {
	case mesos.TaskState_TASK_LOST,
		mesos.TaskState_TASK_FINISHED,
		mesos.TaskState_TASK_KILLED,
		mesos.TaskState_TASK_ERROR,
		mesos.TaskState_TASK_FAILED:
		// Pump the brakes so that we have time to deconfigure the lost node
		// before adding a new one.  If we don't deconfigure first, we risk
		// split brain.
		s.PumpTheBrakes()
		delete(s.running, etcdConfig.Name)
		go func() {
			rpc.RemoveInstance(s.running, etcdConfig.Name)
			s.QueueLaunchAttempt()
		}()
	case mesos.TaskState_TASK_RUNNING:
		s.running[etcdConfig.Name] = &etcdConfig

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
			s.running[etcdConfig.Name] = &etcdConfig
		}
	default:
		log.Warningf("Received unhandled task state: %+v", status.GetState())
	}

	if len(s.running) == 0 {
		// TODO(tyler) logic for restoring from backup
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

func (s *EtcdScheduler) Initialize(driver scheduler.SchedulerDriver) {
	// Reset mutable state
	log.Info("Initialize outer attempt")
	s.mut.Lock()
	log.Info("Initialize outer locked")
	s.running = map[string]*common.EtcdConfig{}
	s.mut.Unlock()
	log.Info("Initialize outer unlocked")

	// Pump the brakes to allow some time for reconciliation.
	s.PumpTheBrakes()
	s.PumpTheBrakes()

	// Request that the master send us TaskStatus for live tasks.
	backoff := 1
	for retries := 0; retries < 5; retries++ {
		_, err := driver.ReconcileTasks([]*mesos.TaskStatus{})
		if err != nil {
			log.Errorf("Error while calling ReconcileTasks: %s", err)
		} else {
			go func() {
				time.Sleep(4 * s.chillFactor * time.Second)
				log.Info("Initialize inner attempt")
				s.mut.Lock()
				log.Info("Initialize inner locked")
				s.state = Mutable
				s.mut.Unlock()
				log.Info("Initialize inner unlocked")
			}()
			return
		}
		time.Sleep(time.Duration(backoff) * time.Second)
		backoff = int(math.Min(float64(backoff<<1), 8))
	}
	log.Fatal("Failed to call ReconcileTasks!  " +
		"It is dangerous to continue at this point.  Dying.")
}

func (s *EtcdScheduler) QueueLaunchAttempt() {
	select {
	case s.launchChan <- struct{}{}:
	default:
		// Somehow launchChan is full...
		log.Warning("launchChan is full!")
	}
}

func (s *EtcdScheduler) PumpTheBrakes() {
	select {
	case s.pauseChan <- struct{}{}:
	default:
		log.Warning("pauseChan is full!")
	}
}

func (s *EtcdScheduler) PeriodicLaunchRequestor() {
	for {
		s.mut.RLock()
		if len(s.running) < s.desiredInstanceCount &&
			s.state == Mutable {
			s.QueueLaunchAttempt()
		} else if s.state == Immutable {
			log.Info("PeriodicLaunchRequestor skipping due to " +
				"Immutable scheduler state.")
		}
		s.mut.RUnlock()
		time.Sleep(5 * s.chillFactor * time.Second)
	}
}

func (s *EtcdScheduler) PeriodicPruner() {
	for {
		s.mut.RLock()
		runningCopy := map[string]*common.EtcdConfig{}
		for k, v := range s.running {
			runningCopy[k] = v
		}
		s.mut.RUnlock()
		if s.state == Mutable {
			configuredMembers, err := rpc.MemberList(runningCopy)
			if err != nil {
				log.Errorf("PeriodicPruner could not retrieve current member list: %s",
					err)
			} else {
				s.mut.RLock()
				for k, _ := range configuredMembers {
					_, present := s.running[k]
					if !present {
						// TODO(tyler) only do this after we've allowed time to settle after
						// reconciliation, or we will nuke valid nodes!
						log.Warningf("PeriodicPruner attempting to deconfigure unknown etcd "+
							"instance: %s", k)
						rpc.RemoveInstance(s.running, k)
					}
				}
				s.mut.RUnlock()
			}
		} else {
			log.Info("PeriodicPruner skipping due to Immutable scheduler state.")
		}
		time.Sleep(4 * s.chillFactor * time.Second)
	}
}

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
				log.Infof("SerialLauncher sleeping for %d seconds "+
					"after receiving pause signal.", s.chillFactor)
				time.Sleep(s.chillFactor * time.Second)
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

			// Wait some time between launches to allow a cluster to settle.
			log.Infof("SerialLauncher sleeping for %d seconds after "+
				"launch attempt.", s.chillFactor)
			time.Sleep(s.chillFactor * time.Second)
		case <-s.pauseChan:
			log.Infof("SerialLauncher sleeping for %d seconds "+
				"after receiving pause signal.", s.chillFactor)
			time.Sleep(s.chillFactor * time.Second)
		}
	}
}

// TODO(tyler) split this long function up!
func (s *EtcdScheduler) launchOne(driver scheduler.SchedulerDriver) {
	log.Infoln("Attempting to launch a task.")
	s.mut.RLock()

	log.Infof(
		"running instances: %d desired: %d offers: %d",
		len(s.running), s.desiredInstanceCount, s.offerCache.Len(),
	)
	log.Infof("running: %+v", s.running)
	if len(s.running) >= s.desiredInstanceCount {
		log.Infoln("Already running enough tasks.")
		s.mut.RUnlock()
		return
	}

	members, err := rpc.MemberList(s.running)
	if err != nil {
		log.Errorf("Failed to retrieve running member list, "+
			"rescheduling launch attempt for later: %s", err)
		s.PumpTheBrakes()
		s.QueueLaunchAttempt()
		s.mut.RUnlock()
		return
	}
	if len(members) == s.desiredInstanceCount {
		log.Errorf("Cluster is already configured for desired number of nodes.  " +
			"Must deconfigure any dead nodes first or we may risk livelock.")
		s.mut.RUnlock()
		return
	}
	err = rpc.HealthCheck(s.running)
	if err != nil && len(s.running) != 0 {
		log.Errorf("Failed health check, rescheduling launch attempt for later: %s", err)
		s.PumpTheBrakes()
		s.QueueLaunchAttempt()
		s.mut.RUnlock()
		return
	}
	s.mut.RUnlock()

	// Issue BlockingPop until we get back an offer we can use.
	var offer *mesos.Offer
	for {
		innerOffer, err := func() (*mesos.Offer, error) {
			offer := s.offerCache.BlockingPop()
			log.Info("statusupdate locked")
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

	if len(s.running) >= s.desiredInstanceCount {
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
	if len(s.running) == 0 {
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
	// TODO(tyler) set data to serialized conf, recompute the rest from our
	// task's resources.
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

	s.mut.Lock()
	s.pending[instance.Name] = struct{}{}
	s.mut.Unlock()

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

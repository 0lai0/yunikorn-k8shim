/*
Copyright 2019 The Unity Scheduler Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rmproxy

import (
    "errors"
    "fmt"
    "github.com/golang/glog"
    "github.infra.cloudera.com/yunikorn/scheduler-interface/lib/go/si"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/api"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/cache/cacheevent"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/common"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/common/commonevents"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/handler"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/rmproxy/rmevent"
    "reflect"
    "sync"
)

// Gateway to talk to ResourceManager (behind grpc/API of scheduler-interface)
type RMProxy struct {
    EventHandlers handler.EventHandlers

    // Internal fields
    pendingRMEvents chan interface{}

    rmIdToCallback map[string]api.ResourceManagerCallback

    lock sync.RWMutex
}

func (m *RMProxy) GetRMEventHandler() commonevents.EventHandler {
    return m
}

func enqueueAndCheckFull(queue chan interface{}, ev interface{}) {
    select {
    case queue <- ev:
        glog.V(2).Infof("Enqueued event=%s, current queue size=%d", ev, len(queue))
    default:
        panic(fmt.Sprintf("Failed to enqueue event=%s", reflect.TypeOf(ev).String()))
    }
}

func (m *RMProxy) HandleEvent(ev interface{}) {
    enqueueAndCheckFull(m.pendingRMEvents, ev)
}

func NewRMProxy() *RMProxy {
    rm := &RMProxy{
        rmIdToCallback:  make(map[string]api.ResourceManagerCallback),
        pendingRMEvents: make(chan interface{}, 1024*1024),
    }
    return rm
}

func (m *RMProxy) StartService(handlers handler.EventHandlers) {
    m.EventHandlers = handlers

    go m.handleRMEvents()
}

func (m *RMProxy) handleRMRecvUpdateResponseError(rmId string, err error) {
    glog.Errorf("Failed to handle response for RM-Id=%s, error=%s", rmId, err)
}

func (m *RMProxy) processUpdateResponse(rmId string, response *si.UpdateResponse) {
    m.lock.RLock()
    m.lock.RUnlock()

    if callback := m.rmIdToCallback[rmId]; callback != nil {
        if err := callback.RecvUpdateResponse(response); err != nil {
            m.handleRMRecvUpdateResponseError(rmId, err)
        }
    } else {
        panic(fmt.Sprintf("Failed to find rm-id=%s in rmproxy", rmId))
    }
}

func (m *RMProxy) processAllocationUpdateEvent(event *rmevent.RMNewAllocationsEvent) {
    if len(event.Allocations) == 0 {
        return
    }
    response := &si.UpdateResponse{
        NewAllocations: event.Allocations,
    }

    m.processUpdateResponse(event.RMId, response)
}

func (m *RMProxy) processApplicationUpdateEvent(event *rmevent.RMApplicationUpdateEvent) {
    if len(event.RejectedApplications) == 0 && len(event.AcceptedApplications) == 0 {
        return
    }
    response := &si.UpdateResponse{
        RejectedApplications: event.RejectedApplications,
        AcceptedApplications: event.AcceptedApplications,
    }

    m.processUpdateResponse(event.RMId, response)
}

func (m *RMProxy) processRMReleaseAllocationEvent(event *rmevent.RMReleaseAllocationEvent) {
    if len(event.ReleasedAllocations) == 0 {
        return
    }
    response := &si.UpdateResponse{
        ReleasedAllocations: event.ReleasedAllocations,
    }

    m.processUpdateResponse(event.RMId, response)
}

func (m *RMProxy) processUpdatePartitionConfigsEvent(event *rmevent.RMRejectedAllocationAskEvent) {
    if len(event.RejectedAllocationAsks) == 0 {
        return
    }
    response := &si.UpdateResponse{
        RejectedAllocations: event.RejectedAllocationAsks,
    }

    m.processUpdateResponse(event.RMId, response)
}

func (m *RMProxy) processRMNodeUpdateEvent(event *rmevent.RMNodeUpdateEvent) {
    if len(event.RejectedNodes) == 0 && len(event.AcceptedNodes) == 0 {
        return
    }
    response := &si.UpdateResponse{
        RejectedNodes: event.RejectedNodes,
        AcceptedNodes: event.AcceptedNodes,
    }

    m.processUpdateResponse(event.RMId, response)
}

func (m *RMProxy) handleRMEvents() {
    for {
        ev := <-m.pendingRMEvents
        switch v := ev.(type) {
        case *rmevent.RMNewAllocationsEvent:
            m.processAllocationUpdateEvent(v)
        case *rmevent.RMApplicationUpdateEvent:
            m.processApplicationUpdateEvent(v)
        case *rmevent.RMReleaseAllocationEvent:
            m.processRMReleaseAllocationEvent(v)
        case *rmevent.RMRejectedAllocationAskEvent:
            m.processUpdatePartitionConfigsEvent(v)
        case *rmevent.RMNodeUpdateEvent:
            m.processRMNodeUpdateEvent(v)
        default:
            panic(fmt.Sprintf("%s is not an acceptable type for RM event.", reflect.TypeOf(v).String()))
        }
    }
}

func (m *RMProxy) RegisterResourceManager(request *si.RegisterResourceManagerRequest, callback api.ResourceManagerCallback) (*si.RegisterResourceManagerResponse, error) {
    m.lock.Lock()
    defer m.lock.Unlock()
    c := make(chan *commonevents.Result, 0)

    if m.rmIdToCallback[request.RmId] != nil {
        // Send to scheduler first, in case we need to remove any partitions.
        go func() {
            m.EventHandlers.SchedulerEventHandler.HandleEvent(
                &commonevents.RemoveRMPartitionsEvent{
                    RmId:    request.RmId,
                    Channel: c,
                })
        }()

        result := <-c
        close(c)
        if !result.Succeeded {
            return nil, errors.New(result.Reason)
        }
    }

    glog.V(0).Infof("Partitions associated with RM=%s removed from the cluster", request.RmId)

    c = make(chan *commonevents.Result, 0)

    // Add new RM.
    go func() {
        m.EventHandlers.CacheEventHandler.HandleEvent(
            &commonevents.RegisterRMEvent{
                RMRegistrationRequest: request,
                Channel:               c,
            })
    }()

    // Wait from channel
    result := <-c
    if result.Succeeded {
        m.rmIdToCallback[request.RmId] = callback
        return &si.RegisterResourceManagerResponse{}, nil
    } else {
        return nil, errors.New(result.Reason)
    }
}

func (m *RMProxy) GetResourceManagerCallback(rmId string) api.ResourceManagerCallback {
    m.lock.RLock()
    defer m.lock.RUnlock()

    return m.rmIdToCallback[rmId]
}

// Do normalize.
func normalizeUpdateRequestByRMId(request *si.UpdateRequest) {
    // Update asks
    if len(request.Asks) > 0 {
        for _, ask := range request.Asks {
            ask.PartitionName = common.GetNormalizedPartitionName(ask.PartitionName, request.RmId)
        }
    }

    // Update Schedulable Nodes
    if len(request.NewSchedulableNodes) > 0 {
        for _, node := range request.NewSchedulableNodes {
            partition := node.Attributes[api.NODE_PARTITION]
            node.Attributes[api.NODE_PARTITION] = common.GetNormalizedPartitionName(partition, request.RmId)
        }
    }

    // Update New apps
    if len(request.NewApplications) > 0 {
        for _, app := range request.NewApplications {
            app.PartitionName = common.GetNormalizedPartitionName(app.PartitionName, request.RmId)
        }
    }

    // Update Updated nodes
    if len(request.UpdatedNodes) > 0 {
        for _, node := range request.UpdatedNodes {
            partition := node.Attributes[api.NODE_PARTITION]
            node.Attributes[api.NODE_PARTITION] = common.GetNormalizedPartitionName(partition, request.RmId)
        }
    }

    // Update Remove apps
    if len(request.RemoveApplications) > 0 {
        for _, app := range request.RemoveApplications {
            app.PartitionName = common.GetNormalizedPartitionName(app.PartitionName, request.RmId)
        }
    }

    // Update releases
    if request.Releases != nil {
        if len(request.Releases.AllocationsToRelease) > 0 {
            for _, rel := range request.Releases.AllocationsToRelease {
                rel.PartitionName = common.GetNormalizedPartitionName(rel.PartitionName, request.RmId)
            }
        }

        if len(request.Releases.AllocationAsksToRelease) > 0 {
            for _, rel := range request.Releases.AllocationAsksToRelease {
                rel.PartitionName = common.GetNormalizedPartitionName(rel.PartitionName, request.RmId)
            }
        }
    }
}

func (m *RMProxy) checkUpdateRequest(request *si.UpdateRequest) error {
    // is it a valid RM id?
    m.lock.RLock()
    defer m.lock.RUnlock()

    if m.rmIdToCallback[request.RmId] == nil {
        return errors.New(fmt.Sprintf("Received UpdateRequest, but RmId=\"%s\" not registered", request.RmId))
    }

    return nil
}

func (m *RMProxy) Update(request *si.UpdateRequest) error {
    if err := m.checkUpdateRequest(request); err != nil {
        return err
    }

    go func() {
        normalizeUpdateRequestByRMId(request)
        m.EventHandlers.CacheEventHandler.HandleEvent(&cacheevent.RMUpdateRequestEvent{Request: request})
    }()

    return nil
}

// Triggers scheduler to reload configuration and apply the changes on-the-fly to the scheduler itself.
func (m *RMProxy) ReloadConfiguration(rmId string) error {
    m.lock.RLock()
    defer m.lock.RUnlock()

    // Trigger an update event, this is not immediately updating configuration,
    // the actual reload happens when scheduler detects configuration file changes
    glog.V(0).Infof("Configuration for RM %s update started", rmId)
    c := make(chan *commonevents.Result, 0)
    m.EventHandlers.CacheEventHandler.HandleEvent(
        &commonevents.ConfigUpdateRMEvent{
            RmId:    rmId,
            Channel: c,
        })

    // logging the result asynchronously
    go func() {
        // Wait from channel
        result := <-c
        glog.V(0).Infof("Configuration for RM %s update finished with result: %v", rmId, result)
    }()

    return nil
}
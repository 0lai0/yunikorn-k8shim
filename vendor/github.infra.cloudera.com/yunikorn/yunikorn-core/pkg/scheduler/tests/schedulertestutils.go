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

package tests

import (
    "fmt"
    "github.infra.cloudera.com/yunikorn/scheduler-interface/lib/go/si"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/cache"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/common/resources"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/scheduler"
    "sync"
    "testing"
    "time"
)

type MockRMCallbackHandler struct {
    t *testing.T

    acceptedApplications map[string]bool
    rejectedApplications map[string]bool
    acceptedNodes        map[string]bool
    Allocations          map[string]*si.Allocation

    lock sync.RWMutex
}

func NewMockRMCallbackHandler(t *testing.T) *MockRMCallbackHandler {
    return &MockRMCallbackHandler{
        t:                    t,
        acceptedApplications: make(map[string]bool),
        rejectedApplications: make(map[string]bool),
        acceptedNodes:        make(map[string]bool),
        Allocations:          make(map[string]*si.Allocation),
    }
}

func (m *MockRMCallbackHandler) RecvUpdateResponse(response *si.UpdateResponse) error {
    m.lock.Lock()
    defer m.lock.Unlock()

    // m.t.Logf("---- Received Update=%s", strings.PrettyPrintStruct(response))

    for _, app := range response.AcceptedApplications {
        m.acceptedApplications[app.ApplicationId] = true
    }

    for _, app := range response.RejectedApplications {
        m.rejectedApplications[app.ApplicationId] = true
    }

    for _, node := range response.AcceptedNodes {
        m.acceptedNodes[node.NodeId] = true
    }

    for _, alloc := range response.NewAllocations {
        m.Allocations[alloc.Uuid] = alloc
    }

    for _, alloc := range response.ReleasedAllocations {
        delete(m.Allocations, alloc.AllocationUUID)
    }

    return nil
}

func waitForAcceptedApplications(m *MockRMCallbackHandler, appId string, timeoutMs int) {
    var i = 0
    for {
        i++

        m.lock.RLock()
        accepted := m.acceptedApplications[appId]
        m.lock.RUnlock()

        if !accepted {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            m.t.Fatalf("Failed to wait AcceptedApplications.")
            return
        }
    }
}

func waitForRejectedApplications(m *MockRMCallbackHandler, appId string, timeoutMs int) {
    var i = 0
    for {
        i++

        m.lock.RLock()
        wait := !m.rejectedApplications[appId] || m.acceptedApplications[appId]
        m.lock.RUnlock()

        if wait {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            m.t.Fatalf("Failed to wait RejectedApplications.")
            return
        }
    }
}

func waitForAcceptedNodes(m *MockRMCallbackHandler, nodeId string, timeoutMs int) {
    var i = 0
    for {
        i++

        m.lock.RLock()
        accepted := m.acceptedNodes[nodeId]
        m.lock.RUnlock()

        if !accepted {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            m.t.Fatalf("Failed to wait AcceptedNode.")
            return
        }
    }
}

func waitForPendingResource(t *testing.T, queue *scheduler.SchedulingQueue, memory resources.Quantity, timeoutMs int) {
    var i = 0
    for {
        i++
        if queue.GetPendingResource().Resources[resources.MEMORY] != memory {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            t.Fatalf("Failed to wait pending resource, actual = %v, expected = %v", queue.GetPendingResource().Resources[resources.MEMORY], memory)
            return
        }
    }
}

func waitForPendingResourceForApplication(t *testing.T, app *scheduler.SchedulingApplication, memory resources.Quantity, timeoutMs int) {
    var i = 0
    for {
        i++
        if app.Requests.GetPendingResource().Resources[resources.MEMORY] != memory {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            t.Fatalf("Failed to wait pending resource, expected=%v, actual=%v", memory, app.Requests.GetPendingResource().Resources[resources.MEMORY])
            return
        }
    }
}

func waitForAllocations(m *MockRMCallbackHandler, nAlloc int, timeoutMs int) {
    var i = 0
    for {
        i++
        m.lock.RLock()
        allocLen := len(m.Allocations)
        m.lock.RUnlock()


        if allocLen != nAlloc {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            m.t.Fatalf("Failed to wait Allocations")
            return
        }
    }
}

func waitForNodesAllocatedResource(t *testing.T, cache *cache.ClusterInfo, partitionName string, nodeIds []string, allocatdMemory resources.Quantity, timeoutMs int) {
    var i = 0
    for {
        i++

        var totalNodeResource resources.Quantity = 0
        for _, nodeId := range nodeIds {
            totalNodeResource += cache.GetPartition(partitionName).GetNode(nodeId).GetAllocatedResource().Resources[resources.MEMORY]
        }

        if totalNodeResource != allocatdMemory {
            time.Sleep(time.Duration(100 * time.Millisecond))
        } else {
            return
        }
        if i*100 >= timeoutMs {
            t.Fatalf("Failed to wait Allocations")
            return
        }
    }
}

func getApplicationInfoFromPartition(partitionInfo *cache.PartitionInfo, appId string) (*cache.ApplicationInfo, error){
    for _, appInfo := range partitionInfo.GetApplications() {
        if appInfo.ApplicationId == appId {
            return appInfo, nil
        }
    }
    return nil, fmt.Errorf("cannot find app %s from cache", appId)
}

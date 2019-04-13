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

package schedulerevent

import (
    "github.infra.cloudera.com/yunikorn/scheduler-interface/lib/go/si"
    "github.infra.cloudera.com/yunikorn/yunikorn-core/pkg/common/commonevents"
)

// From Cache, update about allocations.
type SchedulerAllocationUpdatesEvent struct {
    RejectedAllocations []*commonevents.AllocationProposal
    NewAsks             []*si.AllocationAsk
    ToReleases          *si.AllocationReleasesRequest
}

// From Cache, update about apps.
type SchedulerApplicationsUpdateEvent struct {
    // Type is *cache.ApplicationInfo, avoid cycle imports
    AddedApplications   []interface{}
    RemovedApplications []*si.RemoveApplicationRequest
}

type SchedulerUpdatePartitionsConfigEvent struct {
    // Type is *cache.PartitionInfo, avoid cycle imports
    UpdatedPartitions []interface{}
    ResultChannel     chan *commonevents.Result
}

type SchedulerDeletePartitionsConfigEvent struct {
    // Type is *cache.PartitionInfo, avoid cycle imports
    DeletePartitions  []interface{}
    ResultChannel     chan *commonevents.Result
}
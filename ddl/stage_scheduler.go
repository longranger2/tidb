// Copyright 2023 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ddl

import (
	"context"
	"encoding/json"

	"github.com/pingcap/errors"
	"github.com/pingcap/tidb/ddl/ingest"
	"github.com/pingcap/tidb/disttask/framework/proto"
	"github.com/pingcap/tidb/disttask/framework/scheduler"
	"github.com/pingcap/tidb/disttask/framework/scheduler/execute"
	"github.com/pingcap/tidb/parser/model"
	"github.com/pingcap/tidb/table"
	"github.com/pingcap/tidb/util/logutil"
	"go.uber.org/zap"
)

// BackfillGlobalMeta is the global task meta for backfilling index.
type BackfillGlobalMeta struct {
	Job        model.Job `json:"job"`
	EleID      int64     `json:"ele_id"`
	EleTypeKey []byte    `json:"ele_type_key"`

	CloudStorageURI string `json:"cloud_storage_uri"`
}

// BackfillSubTaskMeta is the sub-task meta for backfilling index.
type BackfillSubTaskMeta struct {
	PhysicalTableID int64  `json:"physical_table_id"`
	StartKey        []byte `json:"start_key"`
	EndKey          []byte `json:"end_key"`

	DataFiles      []string `json:"data_files"`
	StatFiles      []string `json:"stat_files"`
	RangeSplitKeys [][]byte `json:"range_split_keys"`
	MinKey         []byte   `json:"min_key"`
	MaxKey         []byte   `json:"max_key"`
	TotalKVSize    uint64   `json:"total_kv_size"`
}

// NewBackfillSchedulerHandle creates a new backfill scheduler.
func NewBackfillSchedulerHandle(ctx context.Context, taskMeta []byte, d *ddl,
	stage int64, summary *execute.Summary) (execute.SubtaskExecutor, error) {
	bgm := &BackfillGlobalMeta{}
	err := json.Unmarshal(taskMeta, bgm)
	if err != nil {
		return nil, err
	}
	jobMeta := &bgm.Job

	_, tbl, err := d.getTableByTxn(d.store, jobMeta.SchemaID, jobMeta.TableID)
	if err != nil {
		return nil, err
	}
	indexInfo := model.FindIndexInfoByID(tbl.Meta().Indices, bgm.EleID)
	if indexInfo == nil {
		logutil.BgLogger().Warn("index info not found", zap.String("category", "ddl-ingest"),
			zap.Int64("table ID", tbl.Meta().ID), zap.Int64("index ID", bgm.EleID))
		return nil, errors.New("index info not found")
	}

	bc, err := ingest.LitBackCtxMgr.Register(ctx, indexInfo.Unique, jobMeta.ID, d.etcdCli, jobMeta.ReorgMeta.ResourceGroupName)
	if err != nil {
		return nil, errors.Trace(err)
	}

	switch stage {
	case proto.StepInit:
		jc := d.jobContext(jobMeta.ID, jobMeta.ReorgMeta)
		d.setDDLLabelForTopSQL(jobMeta.ID, jobMeta.Query)
		d.setDDLSourceForDiagnosis(jobMeta.ID, jobMeta.Type)
		return newReadIndexStage(
			d, &bgm.Job, indexInfo, tbl.(table.PhysicalTable), jc, bc, summary, bgm.CloudStorageURI), nil
	case proto.StepOne:
		if len(bgm.CloudStorageURI) > 0 {
			return newMergeSortStage(jobMeta.ID, indexInfo, tbl.(table.PhysicalTable), bc, bgm.CloudStorageURI)
		}
		return newIngestIndexStage(jobMeta.ID, indexInfo, tbl.(table.PhysicalTable), bc), nil
	default:
		return nil, errors.Errorf("unknown step %d for job %d", stage, jobMeta.ID)
	}
}

// BackfillTaskType is the type of backfill task.
const BackfillTaskType = "backfill"

type backfillDistScheduler struct {
	*scheduler.BaseScheduler
	d *ddl
}

func newBackfillDistScheduler(ctx context.Context, id string, taskID int64, taskTable scheduler.TaskTable, d *ddl) scheduler.Scheduler {
	s := &backfillDistScheduler{
		BaseScheduler: scheduler.NewBaseScheduler(ctx, id, taskID, taskTable),
		d:             d,
	}
	s.BaseScheduler.Extension = s
	return s
}

func (s *backfillDistScheduler) GetSubtaskExecutor(ctx context.Context, task *proto.Task, summary *execute.Summary) (execute.SubtaskExecutor, error) {
	switch task.Step {
	case proto.StepInit, proto.StepOne:
		return NewBackfillSchedulerHandle(ctx, task.Meta, s.d, task.Step, summary)
	default:
		return nil, errors.Errorf("unknown backfill step %d for task %d", task.Step, task.ID)
	}
}

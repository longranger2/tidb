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

package framework_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/pingcap/failpoint"
	"github.com/pingcap/tidb/disttask/framework/dispatcher"
	"github.com/pingcap/tidb/disttask/framework/mock"
	mockexecute "github.com/pingcap/tidb/disttask/framework/mock/execute"
	"github.com/pingcap/tidb/disttask/framework/proto"
	"github.com/pingcap/tidb/disttask/framework/scheduler"
	"github.com/pingcap/tidb/disttask/framework/storage"
	"github.com/pingcap/tidb/domain/infosync"
	"github.com/pingcap/tidb/testkit"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type testDispatcherExt struct {
	cnt int
}

var _ dispatcher.Extension = (*testDispatcherExt)(nil)

func (*testDispatcherExt) OnTick(_ context.Context, _ *proto.Task) {
}

func (dsp *testDispatcherExt) OnNextSubtasksBatch(_ context.Context, _ dispatcher.TaskHandle, gTask *proto.Task) (metas [][]byte, err error) {
	if gTask.Step == proto.StepInit {
		dsp.cnt = 3
		return [][]byte{
			[]byte("task1"),
			[]byte("task2"),
			[]byte("task3"),
		}, nil
	}
	if gTask.Step == proto.StepOne {
		dsp.cnt = 4
		return [][]byte{
			[]byte("task4"),
		}, nil
	}
	return nil, nil
}

func (*testDispatcherExt) OnErrStage(_ context.Context, _ dispatcher.TaskHandle, _ *proto.Task, _ []error) (meta []byte, err error) {
	return nil, nil
}

func (dsp *testDispatcherExt) StageFinished(task *proto.Task) bool {
	if task.Step == proto.StepInit && dsp.cnt >= 3 {
		return true
	}
	if task.Step == proto.StepOne && dsp.cnt >= 4 {
		return true
	}
	return false
}

func (dsp *testDispatcherExt) Finished(task *proto.Task) bool {
	return task.Step == proto.StepOne && dsp.cnt >= 4
}

func generateSchedulerNodes4Test() ([]*infosync.ServerInfo, error) {
	serverInfos := infosync.MockGlobalServerInfoManagerEntry.GetAllServerInfo()
	if len(serverInfos) == 0 {
		return nil, errors.New("not found instance")
	}

	serverNodes := make([]*infosync.ServerInfo, 0, len(serverInfos))
	for _, serverInfo := range serverInfos {
		serverNodes = append(serverNodes, serverInfo)
	}
	return serverNodes, nil
}

func (*testDispatcherExt) GetEligibleInstances(_ context.Context, _ *proto.Task) ([]*infosync.ServerInfo, error) {
	return generateSchedulerNodes4Test()
}

func (*testDispatcherExt) IsRetryableErr(error) bool {
	return true
}

func getMockSubtaskExecutor(ctrl *gomock.Controller) *mockexecute.MockSubtaskExecutor {
	executor := mockexecute.NewMockSubtaskExecutor(ctrl)
	executor.EXPECT().Init(gomock.Any()).Return(nil).AnyTimes()
	executor.EXPECT().Cleanup(gomock.Any()).Return(nil).AnyTimes()
	executor.EXPECT().Rollback(gomock.Any()).Return(nil).AnyTimes()
	executor.EXPECT().OnFinished(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	return executor
}

func RegisterTaskMeta(t *testing.T, ctrl *gomock.Controller, m *sync.Map, dispatcherHandle dispatcher.Extension) {
	mockExtension := mock.NewMockExtension(ctrl)
	mockSubtaskExecutor := getMockSubtaskExecutor(ctrl)
	mockSubtaskExecutor.EXPECT().RunSubtask(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, subtask *proto.Subtask) error {
			switch subtask.Step {
			case proto.StepInit:
				m.Store("0", "0")
			case proto.StepOne:
				m.Store("1", "1")
			default:
				panic("invalid step")
			}
			return nil
		}).AnyTimes()
	mockExtension.EXPECT().GetSubtaskExecutor(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockSubtaskExecutor, nil).AnyTimes()
	registerTaskMetaInner(t, proto.TaskTypeExample, mockExtension, dispatcherHandle)
}

func registerTaskMetaInner(t *testing.T, taskType string, mockExtension scheduler.Extension, dispatcherHandle dispatcher.Extension) {
	t.Cleanup(func() {
		dispatcher.ClearDispatcherFactory()
		scheduler.ClearSchedulers()
	})
	dispatcher.RegisterDispatcherFactory(taskType,
		func(ctx context.Context, taskMgr *storage.TaskManager, serverID string, task *proto.Task) dispatcher.Dispatcher {
			baseDispatcher := dispatcher.NewBaseDispatcher(ctx, taskMgr, serverID, task)
			baseDispatcher.Extension = dispatcherHandle
			return baseDispatcher
		})
	scheduler.RegisterTaskType(taskType,
		func(ctx context.Context, id string, taskID int64, taskTable scheduler.TaskTable) scheduler.Scheduler {
			s := scheduler.NewBaseScheduler(ctx, id, taskID, taskTable)
			s.Extension = mockExtension
			return s
		},
	)
}

func RegisterTaskMetaForExample2(t *testing.T, ctrl *gomock.Controller, m *sync.Map, dispatcherHandle dispatcher.Extension) {
	mockExtension := mock.NewMockExtension(ctrl)
	mockSubtaskExecutor := getMockSubtaskExecutor(ctrl)
	mockSubtaskExecutor.EXPECT().RunSubtask(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, subtask *proto.Subtask) error {
			switch subtask.Step {
			case proto.StepInit:
				m.Store("2", "2")
			case proto.StepOne:
				m.Store("3", "3")
			default:
				panic("invalid step")
			}
			return nil
		}).AnyTimes()
	mockExtension.EXPECT().GetSubtaskExecutor(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockSubtaskExecutor, nil).AnyTimes()
	registerTaskMetaInner(t, proto.TaskTypeExample2, mockExtension, dispatcherHandle)
}

func RegisterTaskMetaForExample3(t *testing.T, ctrl *gomock.Controller, m *sync.Map, dispatcherHandle dispatcher.Extension) {
	mockExtension := mock.NewMockExtension(ctrl)
	mockSubtaskExecutor := getMockSubtaskExecutor(ctrl)
	mockSubtaskExecutor.EXPECT().RunSubtask(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, subtask *proto.Subtask) error {
			switch subtask.Step {
			case proto.StepInit:
				m.Store("4", "4")
			case proto.StepOne:
				m.Store("5", "5")
			default:
				panic("invalid step")
			}
			return nil
		}).AnyTimes()
	mockExtension.EXPECT().GetSubtaskExecutor(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockSubtaskExecutor, nil).AnyTimes()
	registerTaskMetaInner(t, proto.TaskTypeExample3, mockExtension, dispatcherHandle)
}

func DispatchTask(taskKey string, t *testing.T) *proto.Task {
	mgr, err := storage.GetTaskManager()
	require.NoError(t, err)
	taskID, err := mgr.AddNewGlobalTask(taskKey, proto.TaskTypeExample, 8, nil)
	require.NoError(t, err)
	start := time.Now()

	var task *proto.Task
	for {
		if time.Since(start) > 10*time.Minute {
			require.FailNow(t, "timeout")
		}

		time.Sleep(time.Second)
		task, err = mgr.GetGlobalTaskByID(taskID)

		require.NoError(t, err)
		require.NotNil(t, task)
		if task.State != proto.TaskStatePending && task.State != proto.TaskStateRunning && task.State != proto.TaskStateCancelling && task.State != proto.TaskStateReverting {
			break
		}
	}
	return task
}

func DispatchTaskAndCheckSuccess(taskKey string, t *testing.T, m *sync.Map) {
	task := DispatchTask(taskKey, t)
	require.Equal(t, proto.TaskStateSucceed, task.State)
	v, ok := m.Load("1")
	require.Equal(t, true, ok)
	require.Equal(t, "1", v)
	v, ok = m.Load("0")
	require.Equal(t, true, ok)
	require.Equal(t, "0", v)
	m = &sync.Map{}
}

func DispatchAndCancelTask(taskKey string, t *testing.T, m *sync.Map) {
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/MockExecutorRunCancel", "1*return(1)"))
	defer func() {
		require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/MockExecutorRunCancel"))
	}()
	task := DispatchTask(taskKey, t)
	require.Equal(t, proto.TaskStateReverted, task.State)
	m.Range(func(key, value interface{}) bool {
		m.Delete(key)
		return true
	})
}

func DispatchTaskAndCheckState(taskKey string, t *testing.T, m *sync.Map, state string) {
	task := DispatchTask(taskKey, t)
	require.Equal(t, state, task.State)
	m.Range(func(key, value interface{}) bool {
		m.Delete(key)
		return true
	})
}
func DispatchMultiTasksAndOneFail(t *testing.T, num int, m []sync.Map) []*proto.Task {
	var tasks []*proto.Task
	var taskID []int64
	var start []time.Time
	mgr, err := storage.GetTaskManager()
	require.NoError(t, err)
	taskID = make([]int64, num)
	start = make([]time.Time, num)
	tasks = make([]*proto.Task, num)

	for i := 0; i < num; i++ {
		if i == 0 {
			require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/MockExecutorRunErr", "1*return(true)"))
			taskID[0], err = mgr.AddNewGlobalTask("key0", "Example", 8, nil)
			require.NoError(t, err)
			start[0] = time.Now()
			var task *proto.Task
			for {
				if time.Since(start[0]) > 2*time.Minute {
					require.FailNow(t, "timeout")
				}
				time.Sleep(time.Second)
				task, err = mgr.GetGlobalTaskByID(taskID[0])
				tasks[0] = task
				require.NoError(t, err)
				require.NotNil(t, task)
				if task.State != proto.TaskStatePending && task.State != proto.TaskStateRunning && task.State != proto.TaskStateCancelling && task.State != proto.TaskStateReverting {
					break
				}
			}
			require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/MockExecutorRunErr"))
		} else {
			taskID[i], err = mgr.AddNewGlobalTask(fmt.Sprintf("key%d", i), proto.Int2Type(i+2), 8, nil)
			require.NoError(t, err)
			start[i] = time.Now()
		}
	}

	for i := 1; i < num; i++ {
		var task *proto.Task
		for {
			if time.Since(start[i]) > 2*time.Minute {
				require.FailNow(t, "timeout")
			}
			time.Sleep(time.Second)
			task, err = mgr.GetGlobalTaskByID(taskID[i])
			tasks[i] = task
			require.NoError(t, err)
			require.NotNil(t, task)
			if task.State != proto.TaskStatePending && task.State != proto.TaskStateRunning && task.State != proto.TaskStateCancelling && task.State != proto.TaskStateReverting {
				break
			}
		}
	}
	m[0].Range(func(key, value interface{}) bool {
		m[0].Delete(key)
		return true
	})
	return tasks
}

func TestFrameworkBasic(t *testing.T) {
	var m sync.Map
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 2)
	DispatchTaskAndCheckSuccess("key1", t, &m)
	DispatchTaskAndCheckSuccess("key2", t, &m)
	distContext.SetOwner(0)
	time.Sleep(2 * time.Second) // make sure owner changed
	DispatchTaskAndCheckSuccess("key3", t, &m)
	DispatchTaskAndCheckSuccess("key4", t, &m)
	distContext.SetOwner(1)
	time.Sleep(2 * time.Second) // make sure owner changed
	DispatchTaskAndCheckSuccess("key5", t, &m)
	distContext.Close()
}

func TestFramework3Server(t *testing.T) {
	var m sync.Map
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 3)
	DispatchTaskAndCheckSuccess("key1", t, &m)
	DispatchTaskAndCheckSuccess("key2", t, &m)
	distContext.SetOwner(0)
	time.Sleep(2 * time.Second) // make sure owner changed
	DispatchTaskAndCheckSuccess("key3", t, &m)
	DispatchTaskAndCheckSuccess("key4", t, &m)
	distContext.Close()
}

func TestFrameworkAddDomain(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 2)
	DispatchTaskAndCheckSuccess("key1", t, &m)
	distContext.AddDomain()
	DispatchTaskAndCheckSuccess("key2", t, &m)
	distContext.SetOwner(1)
	time.Sleep(2 * time.Second) // make sure owner changed
	DispatchTaskAndCheckSuccess("key3", t, &m)
	distContext.Close()
	distContext.AddDomain()
	DispatchTaskAndCheckSuccess("key4", t, &m)
}

func TestFrameworkDeleteDomain(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 2)
	DispatchTaskAndCheckSuccess("key1", t, &m)
	distContext.DeleteDomain(1)
	time.Sleep(2 * time.Second) // make sure the owner changed
	DispatchTaskAndCheckSuccess("key2", t, &m)
	distContext.Close()
}

func TestFrameworkWithQuery(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 2)
	DispatchTaskAndCheckSuccess("key1", t, &m)

	tk := testkit.NewTestKit(t, distContext.Store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t")
	tk.MustExec("create table t(a int not null, b int not null)")
	rs, err := tk.Exec("select ifnull(a,b) from t")
	require.NoError(t, err)
	fields := rs.Fields()
	require.Greater(t, len(fields), 0)
	require.Equal(t, "ifnull(a,b)", rs.Fields()[0].Column.Name.L)
	require.NoError(t, rs.Close())
	distContext.Close()
}

func TestFrameworkCancelGTask(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 2)
	DispatchAndCancelTask("key1", t, &m)
	distContext.Close()
}

func TestFrameworkSubTaskFailed(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 1)
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/MockExecutorRunErr", "1*return(true)"))
	defer func() {
		require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/MockExecutorRunErr"))
	}()
	DispatchTaskAndCheckState("key1", t, &m, proto.TaskStateReverted)

	distContext.Close()
}

func TestFrameworkSubTaskInitEnvFailed(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 1)
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockExecSubtaskInitEnvErr", "return()"))
	defer func() {
		require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockExecSubtaskInitEnvErr"))
	}()
	DispatchTaskAndCheckState("key1", t, &m, proto.TaskStateReverted)
	distContext.Close()
}

func TestOwnerChange(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})

	distContext := testkit.NewDistExecutionContext(t, 3)
	dispatcher.MockOwnerChange = func() {
		distContext.SetOwner(0)
	}
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/dispatcher/mockOwnerChange", "1*return(true)"))
	DispatchTaskAndCheckSuccess("😊", t, &m)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/dispatcher/mockOwnerChange"))
	distContext.Close()
}

func TestFrameworkCancelThenSubmitSubTask(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 3)
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/dispatcher/cancelBeforeUpdate", "return()"))
	DispatchTaskAndCheckState("😊", t, &m, proto.TaskStateReverted)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/dispatcher/cancelBeforeUpdate"))
	distContext.Close()
}

func TestSchedulerDownBasic(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})

	distContext := testkit.NewDistExecutionContext(t, 4)
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockCleanScheduler", "return()"))
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockStopManager", "4*return(\":4000\")"))
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockTiDBDown", "return(\":4000\")"))
	DispatchTaskAndCheckSuccess("😊", t, &m)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockCleanScheduler"))
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockTiDBDown"))
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockStopManager"))
	distContext.Close()
}

func TestSchedulerDownManyNodes(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})

	distContext := testkit.NewDistExecutionContext(t, 30)
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockCleanScheduler", "return()"))
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockStopManager", "30*return(\":4000\")"))
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/scheduler/mockTiDBDown", "return(\":4000\")"))
	DispatchTaskAndCheckSuccess("😊", t, &m)
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockCleanScheduler"))
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockTiDBDown"))
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/scheduler/mockStopManager"))
	distContext.Close()
}

func TestFrameworkSetLabel(t *testing.T) {
	var m sync.Map
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})
	distContext := testkit.NewDistExecutionContext(t, 3)
	tk := testkit.NewTestKit(t, distContext.Store)
	// 1. all "" role.
	DispatchTaskAndCheckSuccess("😁", t, &m)
	// 2. one "background" role.
	tk.MustExec("set global tidb_service_scope=background")
	tk.MustQuery("select @@global.tidb_service_scope").Check(testkit.Rows("background"))
	tk.MustQuery("select @@tidb_service_scope").Check(testkit.Rows("background"))
	DispatchTaskAndCheckSuccess("😊", t, &m)
	// 3. 2 "background" role.
	tk.MustExec("update mysql.dist_framework_meta set role = \"background\" where host = \":4001\"")
	DispatchTaskAndCheckSuccess("😆", t, &m)

	// 4. set wrong sys var.
	tk.MustMatchErrMsg("set global tidb_service_scope=wrong", `incorrect value: .*. tidb_service_scope options: "", background`)

	// 5. set keyspace id.
	tk.MustExec("update mysql.dist_framework_meta set keyspace_id = 16777216 where host = \":4001\"")
	tk.MustQuery("select keyspace_id from mysql.dist_framework_meta where host = \":4001\"").Check(testkit.Rows("16777216"))

	distContext.Close()
}

func TestMultiTasks(t *testing.T) {
	defer dispatcher.ClearDispatcherFactory()
	defer scheduler.ClearSchedulers()
	num := 3

	m := make([]sync.Map, num)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &(m[0]), &testDispatcherExt{})
	RegisterTaskMetaForExample2(t, ctrl, &(m[1]), &testDispatcherExt{})
	RegisterTaskMetaForExample3(t, ctrl, &(m[2]), &testDispatcherExt{})

	distContext := testkit.NewDistExecutionContext(t, 3)
	tasks := DispatchMultiTasksAndOneFail(t, num, m)
	require.Equal(t, proto.TaskStateReverted, tasks[0].State)
	v, ok := m[0].Load("0")
	require.Equal(t, false, ok)
	require.Equal(t, nil, v)
	v, ok = m[0].Load("1")
	require.Equal(t, false, ok)
	require.Equal(t, nil, v)
	require.Equal(t, proto.TaskStateSucceed, tasks[1].State)
	v, ok = m[1].Load("2")
	require.Equal(t, true, ok)
	require.Equal(t, "2", v)
	v, ok = m[1].Load("3")
	require.Equal(t, true, ok)
	require.Equal(t, "3", v)
	require.Equal(t, proto.TaskStateSucceed, tasks[2].State)
	v, ok = m[2].Load("4")
	require.Equal(t, true, ok)
	require.Equal(t, "4", v)
	v, ok = m[2].Load("5")
	require.Equal(t, true, ok)
	require.Equal(t, "5", v)
	distContext.Close()
}

func TestGC(t *testing.T) {
	var m sync.Map

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	RegisterTaskMeta(t, ctrl, &m, &testDispatcherExt{})

	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/storage/subtaskHistoryKeepSeconds", "return(1)"))
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/disttask/framework/dispatcher/historySubtaskTableGcInterval", "return(1)"))
	defer func() {
		require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/storage/subtaskHistoryKeepSeconds"))
		require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/disttask/framework/dispatcher/historySubtaskTableGcInterval"))
	}()

	distContext := testkit.NewDistExecutionContext(t, 3)
	DispatchTaskAndCheckSuccess("😊", t, &m)

	mgr, err := storage.GetTaskManager()
	require.NoError(t, err)

	var historySubTasksCnt int
	require.Eventually(t, func() bool {
		historySubTasksCnt, err = storage.GetSubtasksFromHistoryForTest(mgr)
		if err != nil {
			return false
		}
		return historySubTasksCnt == 4
	}, 10*time.Second, 500*time.Millisecond)

	dispatcher.WaitTaskFinished <- struct{}{}

	require.Eventually(t, func() bool {
		historySubTasksCnt, err := storage.GetSubtasksFromHistoryForTest(mgr)
		if err != nil {
			return false
		}
		return historySubTasksCnt == 0
	}, 10*time.Second, 500*time.Millisecond)

	distContext.Close()
}

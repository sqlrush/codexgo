package tools

import (
	"strings"
	"testing"
)

func TestRenderGSMem(t *testing.T) {
	d := &gsmemData{
		Target: "x", SharedBuf: "4096 MB", HitRatio: 99.49,
		Types:    []memTypeRow{{Type: "max_process_memory", MB: 12288}, {Type: "shared_used_memory", MB: 1300}},
		Sessions: []sessMemRow{{Sessid: "123.456", PID: "456", UsedMB: "10.5", TotalMB: "20.1"}},
	}
	out := renderGSMem(d)
	for _, want := range []string{"引擎内存", "shared_buffers", "99.49%  (优)", "引擎内存(按类型)", "Top 会话内存"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderGSMem missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
	if hitGrade(99) != "优" || hitGrade(96) != "良" || hitGrade(80) != "过低" {
		t.Errorf("hitGrade wrong")
	}
}

func TestRenderWAL(t *testing.T) {
	d := &walData{
		Target: "x", CurrentLSN: "14A/6BB960F0", CkTimed: 7619, CkReq: 2,
		WriteMS: "0", SyncMS: "1401601",
		Settings: [][]string{{"wal_level", "logical"}, {"archive_mode", "off"}},
	}
	out := renderWAL(d)
	for _, want := range []string{"WAL / 检查点", "14A/6BB960F0", "检查点", "请求式占比", "关键参数", "wal_level"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderWAL missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderWALReqWarn(t *testing.T) {
	d := &walData{Target: "x", CkTimed: 10, CkReq: 90} // 90% requested → warn
	out := renderWAL(d)
	if !strings.Contains(out, "偏高") {
		t.Errorf("high requested-checkpoint ratio should warn:\n%s", out)
	}
}

func TestRenderReplicationStandalone(t *testing.T) {
	d := &replData{Target: "x", InRecovry: false}
	out := renderReplication(d)
	for _, want := range []string{"流复制", "主库 / 单机", "无备库连接", "无复制槽"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderReplication missing %q\n%s", want, out)
		}
	}
}

func TestRenderReplicationPrimary(t *testing.T) {
	d := &replData{
		Target:   "x",
		Replicas: [][]string{{"1001", "10.0.0.2", "Streaming", "1/A0", "1/9F", "1/9E", "Sync"}},
		Slots:    [][]string{{"slot1", "physical", "t", "1/A0"}},
	}
	out := renderReplication(d)
	for _, want := range []string{"10.0.0.2", "Streaming", "Sync", "slot1"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderReplication missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderBgWorker(t *testing.T) {
	d := &bgworkerData{Target: "x", ArchFailed: "3", Rows: []bgworkerRow{
		{Thread: "CheckPointer", Count: "1", Statuses: "none"},
		{Thread: "PageWriter", Count: "5", Statuses: "none"},
	}}
	out := renderBgWorker(d)
	for _, want := range []string{"后台进程", "CheckPointer", "归档失败累计 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderBgWorker missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

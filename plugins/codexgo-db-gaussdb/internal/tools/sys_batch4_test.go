package tools

import (
	"strings"
	"testing"
)

func TestRenderResource(t *testing.T) {
	d := &resourceData{
		Target: "x",
		Gauges: []resGauge{
			{Label: "连接数", Cur: 180, Max: 200, HasMax: true},
			{Label: "WAL 发送器", Cur: 0, Max: 4, HasMax: true},
		},
		Limits: []kv{{"max_worker_processes", "N/A"}, {"autovacuum_max_workers", "3"}},
	}
	out := renderResource(d)
	for _, want := range []string{"资源限额", "使用率", "连接数", "并行/工作进程限额", "N/A"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderResource missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "超 80%") { // 180/200 = 90% → warn
		t.Errorf("over-80%% should warn:\n%s", out)
	}
}

func TestRenderOS(t *testing.T) {
	d := &osData{Target: "x", Rows: [][]string{{"LOAD", ".15"}, {"NUM_CPUS", "18"}}}
	out := renderOS(d)
	for _, want := range []string{"主机 OS 指标", "LOAD", "NUM_CPUS"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderOS missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderOSError(t *testing.T) {
	d := &osData{Target: "x", Err: "permission denied"}
	out := renderOS(d)
	if !strings.Contains(out, "无法读取") || !strings.Contains(out, "MONADMIN") {
		t.Errorf("os error should degrade gracefully:\n%s", out)
	}
}

func TestRenderUsers(t *testing.T) {
	d := &usersData{
		Target: "x", Supers: 1, Expired: 1,
		Rows: []userRow{
			{Name: "omm", Login: true, Super: true, ValidUntil: "", DaysLeft: ""},
			{Name: "app", Login: true, Super: false, ValidUntil: "2026-01-01 00:00:00+08", DaysLeft: "-159", Expired: true},
		},
	}
	out := renderUsers(d)
	for _, want := range []string{"用户 / 角色", "omm", "永久", "已过期", "超级用户 1 个", "1 个密码已过期"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderUsers missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
	if !truthy("t") || !truthy("true") || truthy("f") {
		t.Errorf("truthy wrong")
	}
}

func TestRenderAlert(t *testing.T) {
	d := &alertData{Target: "x", Rows: []alertRow{
		{DB: "postgres", Deadlocks: "0", Conflicts: "0", TempFiles: "223", TempPretty: "359 MB", Sev: statusWarn},
		{DB: "app", Deadlocks: "5", Conflicts: "0", TempFiles: "0", TempPretty: "0 bytes", Sev: statusFail},
	}}
	out := renderAlert(d)
	for _, want := range []string{"数据库告警", "postgres", "359 MB", "严重"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderAlert missing %q\n%s", want, out)
		}
	}
	assertTablesAligned(t, out)
}

func TestRenderAlertEmpty(t *testing.T) {
	out := renderAlert(&alertData{Target: "x"})
	if !strings.Contains(out, "无死锁") {
		t.Errorf("empty alert should say clean:\n%s", out)
	}
}

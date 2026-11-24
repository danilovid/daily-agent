package app

import (
	"testing"
	"time"

	"daily-agent/internal/agent"
)

func TestAgentStatusView(t *testing.T) {
	st := agent.Status{
		State:           agent.StateReady,
		Version:         agent.PinnedVersion,
		Since:           time.Date(2026, 11, 24, 6, 2, 0, 0, time.UTC),
		ModelConfigured: true,
		Restarts:        2,
	}
	svc := NewAgentService(func() agent.Status { return st }, testClock())
	got := svc.Status()
	want := AgentStatusView{State: "ready", Since: "09:02", Version: agent.PinnedVersion, ModelConfigured: true, Restarts: 2}
	if got != want {
		t.Fatalf("состояние: %+v, ждали %+v", got, want)
	}
	if NewAgentService(nil, testClock()).Status().State != agent.StateStopped {
		t.Error("без надзора агент должен быть остановлен")
	}
	if v := AgentStatusFrom(agent.Status{State: agent.StateStarting}, testClock()); v.Since != "" {
		t.Errorf("без времени: %+v", v)
	}
}

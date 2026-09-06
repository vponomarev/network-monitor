package conntrack

import (
	"testing"
	"time"
)

func TestKernelFailureAfterReportedTimeoutIsNotCountedTwice(t *testing.T) {
	failed := 0
	sm := NewStateMachine(StateMachineConfig{OnEvent: func(_ *Connection, event ConnectionEvent) {
		if event == EventFailed {
			failed++
		}
	}})
	defer sm.Stop()
	evt := &ConnectionEventRaw{SocketID: 1, StartedNS: 10, EventType: EventNew, Direction: DirectionOutgoing}
	sm.ProcessEvent(evt)
	sm.mu.Lock()
	conn := sm.connections[sm.makeKey(evt)]
	conn.State = StateClosed
	conn.ClosedTime = time.Now() // Timeout already reported.
	sm.mu.Unlock()
	evt.EventType = EventFailed
	sm.ProcessEvent(evt)
	if failed != 0 {
		t.Fatal("kernel terminal event duplicated the timeout")
	}
}

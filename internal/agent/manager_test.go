package agent

import (
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/tessro/fab/internal/backend" // Register backends for tests
	"github.com/tessro/fab/internal/project"
)

func newTestProject(name string, maxAgents int) *project.Project {
	p := project.NewProject(name, "/tmp/"+name)
	p.MaxAgents = maxAgents
	return p
}

func TestManager_Create(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	t.Run("creates agent with unique ID", func(t *testing.T) {
		agent, err := m.Create(proj)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if agent.ID == "" {
			t.Error("expected non-empty ID")
		}
		if agent.Project != proj {
			t.Error("expected project to be set")
		}
		if agent.Worktree == nil {
			t.Error("expected worktree to be assigned")
		}
		if agent.GetState() != StateStarting {
			t.Errorf("expected Starting state, got %s", agent.GetState())
		}
	})

	t.Run("assigns different worktrees", func(t *testing.T) {
		m := NewManager()
		proj := newTestProject("test-proj", 3)

		a1, _ := m.Create(proj)
		a2, _ := m.Create(proj)

		if a1.Worktree.Path == a2.Worktree.Path {
			t.Error("expected different worktrees")
		}
	})

	t.Run("returns error when no capacity", func(t *testing.T) {
		m := NewManager()
		proj := newTestProject("small-proj", 1)

		_, err := m.Create(proj)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = m.Create(proj)
		if err != ErrNoCapacity {
			t.Errorf("expected ErrNoCapacity, got %v", err)
		}
	})
}

func TestManager_Get(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	agent, _ := m.Create(proj)

	t.Run("returns existing agent", func(t *testing.T) {
		found, err := m.Get(agent.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found != agent {
			t.Error("expected same agent")
		}
	})

	t.Run("returns error for unknown agent", func(t *testing.T) {
		_, err := m.Get("nonexistent")
		if err != ErrAgentNotFound {
			t.Errorf("expected ErrAgentNotFound, got %v", err)
		}
	})
}

func TestManager_List(t *testing.T) {
	m := NewManager()
	proj1 := newTestProject("proj1", 3)
	proj2 := newTestProject("proj2", 3)

	a1, _ := m.Create(proj1)
	a2, _ := m.Create(proj1)
	a3, _ := m.Create(proj2)

	t.Run("lists all agents", func(t *testing.T) {
		all := m.List("")
		if len(all) != 3 {
			t.Errorf("expected 3 agents, got %d", len(all))
		}
	})

	t.Run("filters by project", func(t *testing.T) {
		proj1Agents := m.List("proj1")
		if len(proj1Agents) != 2 {
			t.Errorf("expected 2 agents for proj1, got %d", len(proj1Agents))
		}

		proj2Agents := m.List("proj2")
		if len(proj2Agents) != 1 {
			t.Errorf("expected 1 agent for proj2, got %d", len(proj2Agents))
		}
	})

	t.Run("returns empty for unknown project", func(t *testing.T) {
		agents := m.List("unknown")
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
	})

	// Silence unused variable warnings
	_ = a1
	_ = a2
	_ = a3
}

func TestManager_ListInfo(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	agent, _ := m.Create(proj)
	agent.SetTask("FAB-42")

	infos := m.ListInfo("")
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}

	info := infos[0]
	if info.ID != agent.ID {
		t.Error("expected matching ID")
	}
	if info.Project != "test-proj" {
		t.Errorf("expected project test-proj, got %s", info.Project)
	}
	if info.Task != "FAB-42" {
		t.Errorf("expected task FAB-42, got %s", info.Task)
	}
}

func TestManager_Count(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 5)

	if m.Count() != 0 {
		t.Error("expected 0 agents initially")
	}

	_, _ = m.Create(proj)
	_, _ = m.Create(proj)
	_, _ = m.Create(proj)

	if m.Count() != 3 {
		t.Errorf("expected 3 agents, got %d", m.Count())
	}

	if m.CountByProject("test-proj") != 3 {
		t.Errorf("expected 3 agents by project, got %d", m.CountByProject("test-proj"))
	}

	if m.CountByProject("other") != 0 {
		t.Errorf("expected 0 agents for other project, got %d", m.CountByProject("other"))
	}
}

func TestManager_CountByState(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 5)

	a1, _ := m.Create(proj)
	a2, _ := m.Create(proj)
	a3, _ := m.Create(proj)

	_ = a1.MarkRunning()
	_ = a2.MarkRunning()
	_ = a2.MarkIdle()
	// a3 remains in Starting

	counts := m.CountByState()
	if counts[StateStarting] != 1 {
		t.Errorf("expected 1 Starting, got %d", counts[StateStarting])
	}
	if counts[StateRunning] != 1 {
		t.Errorf("expected 1 Running, got %d", counts[StateRunning])
	}
	if counts[StateIdle] != 1 {
		t.Errorf("expected 1 Idle, got %d", counts[StateIdle])
	}

	_ = a3 // silence unused
}

func TestManager_Delete(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	agent, _ := m.Create(proj)
	id := agent.ID

	t.Run("deletes existing agent", func(t *testing.T) {
		err := m.Delete(id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = m.Get(id)
		if err != ErrAgentNotFound {
			t.Error("expected agent to be deleted")
		}
	})

	t.Run("deletes worktree", func(t *testing.T) {
		// Worktree should be removed (new model: worktrees are deleted, not released)
		activeCount := proj.ActiveAgentCount()
		if activeCount != 0 {
			t.Errorf("expected 0 active agents, got %d", activeCount)
		}
	})

	t.Run("returns error for unknown agent", func(t *testing.T) {
		err := m.Delete("nonexistent")
		if err != ErrAgentNotFound {
			t.Errorf("expected ErrAgentNotFound, got %v", err)
		}
	})
}

func TestManager_StopAll(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	a1, _ := m.Create(proj)
	a2, _ := m.Create(proj)

	_ = a1.MarkRunning()
	_ = a2.MarkRunning()

	m.StopAll("test-proj")

	// Both should be done
	if a1.GetState() != StateDone {
		t.Errorf("expected a1 Done, got %s", a1.GetState())
	}
	if a2.GetState() != StateDone {
		t.Errorf("expected a2 Done, got %s", a2.GetState())
	}
}

func TestManager_DeleteAll(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	_, _ = m.Create(proj)
	_, _ = m.Create(proj)

	m.DeleteAll("test-proj")

	if m.Count() != 0 {
		t.Errorf("expected 0 agents, got %d", m.Count())
	}
}

func TestManager_ActiveAgents(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 5)

	a1, _ := m.Create(proj) // Starting - active
	a2, _ := m.Create(proj) // Running - active
	a3, _ := m.Create(proj) // Idle - active
	a4, _ := m.Create(proj) // Done - not active

	_ = a2.MarkRunning()
	_ = a3.MarkRunning()
	_ = a3.MarkIdle()
	_ = a4.MarkRunning()
	_ = a4.MarkDone()

	active := m.ActiveAgents()
	if len(active) != 3 {
		t.Errorf("expected 3 active agents, got %d", len(active))
	}

	_ = a1 // silence unused
}

func TestManager_RunningAgents(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	a1, _ := m.Create(proj)
	a2, _ := m.Create(proj)

	_ = a1.MarkRunning()
	_ = a2.MarkRunning()
	_ = a2.MarkIdle()

	running := m.RunningAgents()
	if len(running) != 1 {
		t.Errorf("expected 1 running agent, got %d", len(running))
	}
}

func TestManager_IdleAgents(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	a1, _ := m.Create(proj)
	a2, _ := m.Create(proj)

	_ = a1.MarkRunning()
	_ = a1.MarkIdle()
	_ = a2.MarkRunning()

	idle := m.IdleAgents()
	if len(idle) != 1 {
		t.Errorf("expected 1 idle agent, got %d", len(idle))
	}
}

func TestManager_Events(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	var events []Event
	var mu sync.Mutex

	m.OnEvent(func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	t.Run("emits created event", func(t *testing.T) {
		events = nil
		agent, _ := m.Create(proj)

		mu.Lock()
		defer mu.Unlock()

		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Type != EventCreated {
			t.Errorf("expected Created event, got %s", events[0].Type)
		}
		if events[0].Agent != agent {
			t.Error("expected event to reference created agent")
		}
	})

	t.Run("emits state change event", func(t *testing.T) {
		events = nil
		agent, _ := m.Create(proj)

		_ = agent.MarkRunning()

		mu.Lock()
		defer mu.Unlock()

		// Created + StateChanged
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		if events[1].Type != EventStateChanged {
			t.Errorf("expected StateChanged event, got %s", events[1].Type)
		}
		if events[1].OldState != StateStarting {
			t.Errorf("expected OldState Starting, got %s", events[1].OldState)
		}
		if events[1].NewState != StateRunning {
			t.Errorf("expected NewState Running, got %s", events[1].NewState)
		}
	})

	t.Run("emits deleted event", func(t *testing.T) {
		events = nil
		agent, _ := m.Create(proj)
		id := agent.ID

		_ = m.Delete(id)

		mu.Lock()
		defer mu.Unlock()

		// Created + Deleted
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		if events[1].Type != EventDeleted {
			t.Errorf("expected Deleted event, got %s", events[1].Type)
		}
	})
}

func TestManager_Concurrent(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 100)

	var wg sync.WaitGroup
	var created atomic.Int32

	// Concurrently create agents
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Create(proj)
			if err == nil {
				created.Add(1)
			}
		}()
	}

	wg.Wait()

	if int(created.Load()) != m.Count() {
		t.Errorf("mismatch: created %d, count %d", created.Load(), m.Count())
	}
}

func TestManager_RegisterProject(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	m.RegisterProject(proj)

	// Should be able to list agents for this project (empty list)
	agents := m.List("test-proj")
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestManager_UnregisterProject(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	m.RegisterProject(proj)
	m.UnregisterProject("test-proj")

	// List should still work but return empty
	agents := m.List("test-proj")
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestManager_OnProjectEvent_Registered(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	var receivedEvents []ProjectEvent
	m.OnProjectEvent(func(event ProjectEvent) {
		receivedEvents = append(receivedEvents, event)
	})

	m.RegisterProject(proj)

	if len(receivedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(receivedEvents))
	}
	if receivedEvents[0].Type != ProjectEventRegistered {
		t.Errorf("expected event type %s, got %s", ProjectEventRegistered, receivedEvents[0].Type)
	}
	if receivedEvents[0].Project != proj {
		t.Errorf("expected project %v, got %v", proj, receivedEvents[0].Project)
	}
}

func TestManager_OnProjectEvent_Unregistered(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	m.RegisterProject(proj)

	var receivedEvents []ProjectEvent
	m.OnProjectEvent(func(event ProjectEvent) {
		receivedEvents = append(receivedEvents, event)
	})

	m.UnregisterProject("test-proj")

	if len(receivedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(receivedEvents))
	}
	if receivedEvents[0].Type != ProjectEventUnregistered {
		t.Errorf("expected event type %s, got %s", ProjectEventUnregistered, receivedEvents[0].Type)
	}
	if receivedEvents[0].Project != proj {
		t.Errorf("expected project %v, got %v", proj, receivedEvents[0].Project)
	}
}

func TestManager_OnProjectEvent_UnregisteredNonexistent(t *testing.T) {
	m := NewManager()

	var receivedEvents []ProjectEvent
	m.OnProjectEvent(func(event ProjectEvent) {
		receivedEvents = append(receivedEvents, event)
	})

	// Unregistering a nonexistent project should not emit an event
	m.UnregisterProject("nonexistent")

	if len(receivedEvents) != 0 {
		t.Errorf("expected 0 events for nonexistent project, got %d", len(receivedEvents))
	}
}

func TestManager_OnProjectEvent_MultipleHandlers(t *testing.T) {
	m := NewManager()
	proj := newTestProject("test-proj", 3)

	var count1, count2 int
	m.OnProjectEvent(func(_ ProjectEvent) {
		count1++
	})
	m.OnProjectEvent(func(_ ProjectEvent) {
		count2++
	})

	m.RegisterProject(proj)

	if count1 != 1 {
		t.Errorf("handler 1 expected 1 call, got %d", count1)
	}
	if count2 != 1 {
		t.Errorf("handler 2 expected 1 call, got %d", count2)
	}
}

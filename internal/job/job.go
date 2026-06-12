package job

import (
	"sync"
	"time"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

type Job struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Status    Status      `json:"status"`
	Result    interface{} `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Manager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewManager() *Manager {
	return &Manager{
		jobs: make(map[string]*Job),
	}
}

func (m *Manager) Create(id string, jobType string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := &Job{
		ID:        id,
		Type:      jobType,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.jobs[id] = job
	return job
}

func (m *Manager) Get(id string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

func (m *Manager) Update(id string, status Status, result interface{}, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return
	}
	job.Status = status
	job.Result = result
	job.Error = errMsg
	job.UpdatedAt = time.Now()
}

func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
}

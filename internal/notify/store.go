package notify

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	PriorityNormal = "normal"
	PriorityHigh   = "high"

	StatusPending = "pending"
	StatusSent    = "sent"
	StatusRead    = "read"
)

var (
	ErrNotFound        = errors.New("通知不存在")
	ErrEmptyID         = errors.New("通知编号不能为空")
	ErrDuplicateID     = errors.New("通知编号已存在")
	ErrEmptyRecipient  = errors.New("接收人不能为空")
	ErrEmptyContent    = errors.New("内容不能为空")
	ErrInvalidPriority = errors.New("优先级只能是 normal 或 high")
	ErrInvalidSchedule = errors.New("计划发送时间必须晚于当前时间")
	ErrAlreadySent     = errors.New("通知已发送，不能重复标记")
	ErrNotSent         = errors.New("必须先标记已发送才能标记已读")
	ErrAlreadyRead     = errors.New("通知已读，不能重复标记或回退")
	ErrScheduledNotReached = errors.New("计划发送时间未到，不能标记已发送")
)

type Notification struct {
	ID         string     `json:"id"`
	Recipient  string     `json:"recipient"`
	Content    string     `json:"content"`
	Priority   string     `json:"priority"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	SentAt     *time.Time `json:"sent_at,omitempty"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	ScheduleAt *time.Time `json:"schedule_at,omitempty"`
}

type CreateInput struct {
	ID         string     `json:"id"`
	Recipient  string     `json:"recipient"`
	Content    string     `json:"content"`
	Priority   string     `json:"priority"`
	ScheduleAt *time.Time `json:"schedule_at"`
}

type Store struct {
	mu   sync.RWMutex
	data map[string]*Notification
}

func New() *Store { return &Store{data: make(map[string]*Notification)} }

func trim(s string) string { return strings.TrimSpace(s) }

// clone 返回通知的快照，避免调用方修改内部状态。
func (n *Notification) clone() *Notification {
	c := *n
	return &c
}

func (s *Store) Create(in CreateInput, now time.Time) (*Notification, error) {
	in.ID = trim(in.ID)
	if in.ID == "" {
		return nil, ErrEmptyID
	}
	in.Recipient = trim(in.Recipient)
	if in.Recipient == "" {
		return nil, ErrEmptyRecipient
	}
	in.Content = trim(in.Content)
	if in.Content == "" {
		return nil, ErrEmptyContent
	}
	in.Priority = trim(in.Priority)
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if in.Priority != PriorityNormal && in.Priority != PriorityHigh {
		return nil, ErrInvalidPriority
	}
	if in.ScheduleAt != nil && !in.ScheduleAt.After(now) {
		return nil, ErrInvalidSchedule
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[in.ID]; exists {
		return nil, ErrDuplicateID
	}

	n := &Notification{
		ID:         in.ID,
		Recipient:  in.Recipient,
		Content:    in.Content,
		Priority:   in.Priority,
		Status:     StatusPending,
		CreatedAt:  now,
		ScheduleAt: in.ScheduleAt,
	}
	s.data[in.ID] = n
	return n.clone(), nil
}

func (s *Store) MarkSent(id string, now time.Time) (*Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.data[id]
	if !ok {
		return nil, ErrNotFound
	}
	switch n.Status {
	case StatusSent:
		return nil, ErrAlreadySent
	case StatusRead:
		return nil, ErrAlreadyRead
	}
	// 计划发送时间未到时拒绝标记：只有当前时间到达或超过 ScheduleAt 才允许发送；
	// 没有设置 ScheduleAt 的通知不受影响。
	if n.ScheduleAt != nil && now.Before(*n.ScheduleAt) {
		return nil, ErrScheduledNotReached
	}
	n.Status = StatusSent
	n.SentAt = &now
	return n.clone(), nil
}

func (s *Store) MarkRead(id string, now time.Time) (*Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.data[id]
	if !ok {
		return nil, ErrNotFound
	}
	switch n.Status {
	case StatusPending:
		return nil, ErrNotSent
	case StatusRead:
		return nil, ErrAlreadyRead
	}
	n.Status = StatusRead
	n.ReadAt = &now
	return n.clone(), nil
}

func (s *Store) Get(id string) (*Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.data[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n.clone(), nil
}

func (s *Store) List() []*Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Notification, 0, len(s.data))
	for _, n := range s.data {
		out = append(out, n.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		// 高优先级在前；同优先级内按创建时间倒序；时间相同按编号升序保证稳定。
		if priorityRank(out[i].Priority) != priorityRank(out[j].Priority) {
			return priorityRank(out[i].Priority) < priorityRank(out[j].Priority)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func priorityRank(priority string) int {
	if priority == PriorityHigh {
		return 0
	}
	return 1
}

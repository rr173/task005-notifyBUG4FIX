package notify

import (
	"testing"
	"time"
)

// TestMarkSentScheduleAt 是 MarkSent 必须遵守 ScheduleAt 约束的回归用例。
//
// 预期行为：
//   - 通知带有未来的 ScheduleAt 时，在到达该时间之前 MarkSent 必须被拒绝，
//     且通知保持 pending、SentAt 保持 nil；
//   - 时钟到达 ScheduleAt（now == scheduleAt）时 MarkSent 应当成功；
//   - 没有 ScheduleAt 的通知不受影响，可立即标记发送。
//
// 该用例在修复前的基线代码上必须失败（MarkSent 提前成功返回 nil 错误）。
func TestMarkSentScheduleAt(t *testing.T) {
	store := New()
	base := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	scheduleAt := base.Add(time.Hour) // base + 1h

	// 创建一条带未来计划发送时间的通知
	n, err := store.Create(CreateInput{
		ID:         "S1",
		Recipient:  "user-a",
		Content:    "计划发送",
		Priority:   PriorityNormal,
		ScheduleAt: &scheduleAt,
	}, base)
	if err != nil {
		t.Fatalf("Create 返回错误: %v", err)
	}
	if n.ScheduleAt == nil || !n.ScheduleAt.Equal(scheduleAt) {
		t.Fatalf("ScheduleAt 未正确保存: %+v", n)
	}

	// 1) 在 ScheduleAt 之前调用 MarkSent：必须拒绝
	before := base.Add(30 * time.Minute) // base+30m < scheduleAt(base+1h)
	got, err := store.MarkSent("S1", before)
	if err == nil {
		t.Fatalf("ScheduleAt 未到时 MarkSent 应返回错误，但返回 nil；notification=%+v", got)
	}
	// 通知应仍为 pending，未被改成 sent，SentAt 仍为 nil
	cur, err := store.Get("S1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if cur.Status != StatusPending {
		t.Fatalf("通知应保持 pending，实际 status=%q SentAt=%v", cur.Status, cur.SentAt)
	}
	if cur.SentAt != nil {
		t.Fatalf("SentAt 应为 nil，实际=%v", cur.SentAt)
	}

	// 2) 时钟到达 ScheduleAt（now == scheduleAt）：允许标记发送
	got, err = store.MarkSent("S1", scheduleAt)
	if err != nil {
		t.Fatalf("到达 ScheduleAt 时 MarkSent 应成功，返回错误: %v", err)
	}
	if got.Status != StatusSent || got.SentAt == nil {
		t.Fatalf("期望 sent，实际 %+v", got)
	}

	// 3) 没有 ScheduleAt 的通知不受影响
	store2 := New()
	if _, err := store2.Create(CreateInput{
		ID:        "S2",
		Recipient: "user-b",
		Content:   "立即发送",
		Priority:  PriorityNormal,
	}, base); err != nil {
		t.Fatalf("Create S2 返回错误: %v", err)
	}
	got, err = store2.MarkSent("S2", base)
	if err != nil {
		t.Fatalf("无 ScheduleAt 时 MarkSent 应成功，返回错误: %v", err)
	}
	if got.Status != StatusSent || got.SentAt == nil {
		t.Fatalf("期望 sent，实际 %+v", got)
	}
}

// TestMarkSentScheduleAtAfter 验证时钟超过 ScheduleAt 后 MarkSent 也能成功。
func TestMarkSentScheduleAtAfter(t *testing.T) {
	store := New()
	base := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	scheduleAt := base.Add(time.Hour)
	if _, err := store.Create(CreateInput{
		ID: "SA", Recipient: "u", Content: "c", Priority: PriorityNormal,
		ScheduleAt: &scheduleAt,
	}, base); err != nil {
		t.Fatalf("Create 返回错误: %v", err)
	}
	// 超过 ScheduleAt：应允许标记发送
	got, err := store.MarkSent("SA", scheduleAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("超过 ScheduleAt 时 MarkSent 应成功，返回错误: %v", err)
	}
	if got.Status != StatusSent || got.SentAt == nil {
		t.Fatalf("期望 sent，实际 %+v", got)
	}
}

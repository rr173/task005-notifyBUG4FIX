package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	mu sync.RWMutex
	t  time.Time
}

func (c *testClock) now() time.Time      { c.mu.RLock(); defer c.mu.RUnlock(); return c.t }
func (c *testClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

func doReq(t *testing.T, srv *httptest.Server, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, srv.URL+path, r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

// TestAPIMarkSentRejectsBeforeScheduleAt 是 HTTP 层的回归用例：
// 通过 MarkSent 接口（POST /api/notifications/{id}/send）验证 ScheduleAt 约束。
//
// 预期：在 ScheduleAt 到达之前调用 MarkSent 接口应被拒绝（409 Conflict），
// 通知保持 pending；推进时钟到 ScheduleAt 后再调用应成功（200）。
//
// 该用例在修复前的基线代码上必须失败（接口提前返回 200）。
func TestAPIMarkSentRejectsBeforeScheduleAt(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)}
	api := NewWithClock(clk.now)
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	scheduleAt := clk.now().Add(time.Hour) // base + 1h
	createBody, _ := json.Marshal(map[string]any{
		"id":          "S1",
		"recipient":   "user-a",
		"content":     "你好",
		"schedule_at": scheduleAt,
	})

	// 创建带未来 ScheduleAt 的通知
	resp, _ := doReq(t, srv, http.MethodPost, "/api/notifications", string(createBody))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建通知期望 201，实际 %d", resp.StatusCode)
	}

	// 在 ScheduleAt 之前调用 MarkSent 接口：应被拒绝（基线 bug 返回 200）
	resp, body := doReq(t, srv, http.MethodPost, "/api/notifications/S1/send", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("ScheduleAt 未到时 MarkSent 应返回 409，实际 status=%d body=%s", resp.StatusCode, body)
	}

	// 通知应仍为 pending，SentAt 为 nil
	resp, body = doReq(t, srv, http.MethodGet, "/api/notifications/S1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("查询通知期望 200，实际 %d", resp.StatusCode)
	}
	var getOut struct {
		Notification struct {
			Status string     `json:"status"`
			SentAt *time.Time `json:"sent_at"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(body, &getOut); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, body)
	}
	if getOut.Notification.Status != "pending" || getOut.Notification.SentAt != nil {
		t.Fatalf("通知应保持 pending 且 SentAt=nil，实际 status=%q sent_at=%v",
			getOut.Notification.Status, getOut.Notification.SentAt)
	}

	// 推进时钟到 ScheduleAt，再次调用 MarkSent：应成功
	clk.add(time.Hour)
	resp, body = doReq(t, srv, http.MethodPost, "/api/notifications/S1/send", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("到达 ScheduleAt 后 MarkSent 期望 200，实际 %d body=%s", resp.StatusCode, body)
	}
}

// TestAPIMarkSentNoScheduleAtUnaffected 验证无 ScheduleAt 的通知不受修复影响。
func TestAPIMarkSentNoScheduleAtUnaffected(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)}
	api := NewWithClock(clk.now)
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]any{
		"id": "S2", "recipient": "user-b", "content": "立即发送",
	})
	resp, _ := doReq(t, srv, http.MethodPost, "/api/notifications", string(createBody))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建通知期望 201，实际 %d", resp.StatusCode)
	}

	// 无 ScheduleAt：立即标记发送应成功
	resp, body := doReq(t, srv, http.MethodPost, "/api/notifications/S2/send", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("无 ScheduleAt 时 MarkSent 期望 200，实际 %d body=%s", resp.StatusCode, body)
	}
}

package probe

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, time.Second}, // 防御：非法输入按首败处理
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{5, 16 * time.Second},
		{6, 30 * time.Second}, // 封顶
		{100, 30 * time.Second},
	}
	for _, c := range cases {
		if got := BackoffDelay(c.attempt); got != c.want {
			t.Errorf("BackoffDelay(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	mk := func(v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return h
	}
	t.Run("整数秒", func(t *testing.T) {
		d, ok := RetryAfter(mk("3"))
		if !ok || d != 3*time.Second {
			t.Fatalf("got %v %v", d, ok)
		}
	})
	t.Run("零秒合法", func(t *testing.T) {
		d, ok := RetryAfter(mk("0"))
		if !ok || d != 0 {
			t.Fatalf("got %v %v", d, ok)
		}
	})
	t.Run("超上限钳制60s", func(t *testing.T) {
		d, ok := RetryAfter(mk("3600"))
		if !ok || d != maxRetryAfter {
			t.Fatalf("got %v %v", d, ok)
		}
	})
	t.Run("HTTP-date", func(t *testing.T) {
		d, ok := RetryAfter(mk(time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)))
		if !ok || d <= 0 || d > 5*time.Second {
			t.Fatalf("got %v %v", d, ok)
		}
	})
	t.Run("过去的HTTP-date钳到0", func(t *testing.T) {
		d, ok := RetryAfter(mk(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)))
		if !ok || d != 0 {
			t.Fatalf("got %v %v", d, ok)
		}
	})
	t.Run("负数视为缺失", func(t *testing.T) {
		if _, ok := RetryAfter(mk("-5")); ok {
			t.Fatal("负值不应被采纳")
		}
	})
	t.Run("乱码视为缺失", func(t *testing.T) {
		if _, ok := RetryAfter(mk("soon")); ok {
			t.Fatal("非法值不应被采纳")
		}
	})
	t.Run("缺头", func(t *testing.T) {
		if _, ok := RetryAfter(mk("")); ok {
			t.Fatal("缺失头不应被采纳")
		}
	})
}

func TestRetryDelay(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "7")
	if got := RetryDelay(1, h); got != 7*time.Second {
		t.Errorf("有 Retry-After 应优先: got %v", got)
	}
	if got := RetryDelay(2, http.Header{}); got != 2*time.Second {
		t.Errorf("无 Retry-After 应退避: got %v", got)
	}
}

func TestSleepUntil(t *testing.T) {
	t.Run("过期立即返回", func(t *testing.T) {
		start := time.Now()
		if err := SleepUntil(context.Background(), start.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 50*time.Millisecond {
			t.Fatal("过期 deadline 不应睡眠")
		}
	})
	t.Run("ctx取消中断", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		start := time.Now()
		err := SleepUntil(ctx, start.Add(10*time.Second))
		if err == nil {
			t.Fatal("取消应返回错误")
		}
		if time.Since(start) > time.Second {
			t.Fatal("取消后应立即醒来")
		}
	})
	t.Run("正常睡到点", func(t *testing.T) {
		start := time.Now()
		if err := SleepUntil(context.Background(), start.Add(30*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) < 30*time.Millisecond {
			t.Fatal("未睡满")
		}
	})
}

func TestFmtDelay(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0ms"},
		{500 * time.Millisecond, "500ms"},
		{time.Second, "1s"},
		{1500 * time.Millisecond, "2s"}, // 向上取整
		{30 * time.Second, "30s"},
	}
	for _, c := range cases {
		if got := FmtDelay(c.d); got != c.want {
			t.Errorf("FmtDelay(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

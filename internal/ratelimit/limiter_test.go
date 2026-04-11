package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPerHostFloorEnforced(t *testing.T) {
	l := New(100) // generous global rate
	l.SetHostFloor("api.example.com", 100*time.Millisecond)

	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.Wait(ctx, "api.example.com"); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	// Three requests with 100ms floor → minimum ~200ms elapsed (first is free)
	elapsed := time.Since(start)
	if elapsed < 180*time.Millisecond {
		t.Errorf("floor not enforced: elapsed %s", elapsed)
	}
}

func TestGlobalEnvelopeCaps(t *testing.T) {
	l := New(5) // 5 req/sec global
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 6; i++ {
		if err := l.Wait(ctx, "h1"); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	// 6 requests at 5/sec should take at least 1 second for the last one.
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Errorf("global cap not enforced: elapsed %s", elapsed)
	}
}

func TestReportBackoffBlocksWait(t *testing.T) {
	l := New(100)
	l.SetHostFloor("h", 1*time.Millisecond)
	l.ReportBackoff("h", 2*time.Second)

	err := l.Wait(context.Background(), "h")
	var cd *ErrCooldown
	if !errors.As(err, &cd) {
		t.Fatalf("want ErrCooldown, got %T: %v", err, err)
	}
	if cd.Host != "h" {
		t.Errorf("host: %q", cd.Host)
	}
	if cd.Remaining < 1500*time.Millisecond || cd.Remaining > 2100*time.Millisecond {
		t.Errorf("remaining: %s", cd.Remaining)
	}
}

func TestCooldownExpiresNaturally(t *testing.T) {
	l := New(100)
	l.SetHostFloor("h", 1*time.Millisecond)
	l.ReportBackoff("h", 50*time.Millisecond)

	// Wait for cooldown to pass.
	time.Sleep(60 * time.Millisecond)
	if err := l.Wait(context.Background(), "h"); err != nil {
		t.Fatalf("Wait after cooldown: %v", err)
	}
}

func TestWaitRespectsContextCancel(t *testing.T) {
	l := New(0.01) // very slow
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	// Burn the initial token so the next Wait must actually block.
	_ = l.Wait(context.Background(), "h")
	if err := l.Wait(ctx, "h"); err == nil {
		t.Fatal("expected ctx cancel error")
	}
}

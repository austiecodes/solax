package svc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary is also the service. With helperEnv set, TestHelper acts
// as "solax ... run": "serve" holds the lock until SIGTERM; "fail" exits at
// once with a message.
const helperEnv = "SVC_TEST_HELPER"

func TestHelper(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("helper process only")
	}
	pidFile := os.Getenv("SVC_TEST_PIDFILE")
	if mode == "fail" {
		fmt.Println("listen tcp :1: address already in use")
		os.Exit(3)
	}
	l, err := Acquire("helper", pidFile)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	l.Release()
	os.Exit(0)
}

func helper(t *testing.T, mode string) Service {
	t.Helper()
	dir := t.TempDir()
	s := Service{
		Name:    "helper",
		PIDFile: filepath.Join(dir, "run", "helper.pid"),
		LogFile: filepath.Join(dir, "helper.log"),
		Exe:     os.Args[0],
		Args:    []string{"-test.run=^TestHelper$"},
		Wait:    10 * time.Second,
	}
	t.Setenv(helperEnv, mode)
	t.Setenv("SVC_TEST_PIDFILE", s.PIDFile)
	return s
}

func TestAcquire(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "x.pid")

	if _, ok, err := Running(pidFile); err != nil || ok {
		t.Fatalf("no file: Running = %t, %v; want false", ok, err)
	}
	l, err := Acquire("x", pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, ok, err := Running(pidFile)
	if err != nil || !ok || pid != os.Getpid() {
		t.Fatalf("Running = %d, %t, %v; want %d, true", pid, ok, err, os.Getpid())
	}

	// flock is per open file, so a second Acquire conflicts also in the
	// same process.
	var re *RunningError
	if _, err := Acquire("x", pidFile); !errors.As(err, &re) || re.PID != os.Getpid() {
		t.Fatalf("second Acquire = %v; want RunningError with pid %d", err, os.Getpid())
	}

	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Running(pidFile); ok {
		t.Error("after Release: Running = true")
	}
}

// A pid file that no process locks is stale, whatever pid it holds.
func TestStalePIDFile(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "x.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Running(pidFile); err != nil || ok {
		t.Fatalf("Running = %t, %v; want false", ok, err)
	}
	l, err := Acquire("x", pidFile)
	if err != nil {
		t.Fatalf("Acquire over a stale file: %v", err)
	}
	l.Release()
}

func TestStartStop(t *testing.T) {
	s := helper(t, "serve")
	ctx := context.Background()

	pid, err := s.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := Running(s.PIDFile); !ok || got != pid {
		t.Fatalf("Running = %d, %t; want %d, true", got, ok, pid)
	}

	var re *RunningError
	if _, err := s.Start(ctx); !errors.As(err, &re) || re.PID != pid {
		t.Errorf("second Start = %v; want RunningError with pid %d", err, pid)
	}

	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Running(s.PIDFile); ok {
		t.Error("after Stop: Running = true")
	}
	if err := s.Stop(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("second Stop = %v; want ErrNotRunning", err)
	}
}

func TestStartReady(t *testing.T) {
	s := helper(t, "serve")
	calls := 0
	s.Ready = func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	}
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	if calls != 3 {
		t.Errorf("Ready calls = %d, want 3", calls)
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	s := helper(t, "fail")
	// An older run left text in the log; only the new text is reported.
	if err := os.WriteFile(s.LogFile, []byte("old run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start: want an error")
	}
	if msg := err.Error(); !strings.Contains(msg, "address already in use") || strings.Contains(msg, "old run") {
		t.Errorf("Start error = %q; want only the new log text", msg)
	}
}

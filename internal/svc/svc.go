// Package svc runs a solax subcommand as a background service, for
// example "solax server run" or "solax daemon run".
//
// The running process holds an exclusive flock on its pid file (Acquire).
// The lock, not the pid, tells if the service runs. A stale pid file, or a
// pid that the OS gave to another process, never looks like a running
// service, so Stop never signals the wrong process. The OS releases the
// lock when the process ends, also on a crash.
package svc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrNotRunning is returned by Stop when the service does not run.
var ErrNotRunning = errors.New("not running")

// RunningError is returned when a process holds the lock already.
type RunningError struct {
	Name string
	PID  int
}

func (e *RunningError) Error() string {
	if e.PID == 0 {
		return e.Name + " already runs"
	}
	return fmt.Sprintf("%s already runs (pid %d)", e.Name, e.PID)
}

// Lock is the pid file lock of a running service.
type Lock struct{ f *os.File }

// Acquire takes the pid file lock for this process and writes its pid. It
// returns a *RunningError if another process holds the lock.
func Acquire(name, pidFile string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(pidFile, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			pid, _ := readPID(pidFile)
			return nil, &RunningError{Name: name, PID: pid}
		}
		return nil, fmt.Errorf("lock %s: %w", pidFile, err)
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release removes the pid file and releases the lock.
func (l *Lock) Release() error {
	// Remove first: a new process that opens the path after this point
	// creates a new file and locks that one.
	err := os.Remove(l.f.Name())
	return errors.Join(err, l.f.Close())
}

// Running reports if a process holds the lock of pidFile, and its pid.
func Running(pidFile string) (pid int, ok bool, err error) {
	f, err := os.Open(pidFile)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	switch {
	case err == nil:
		// Nobody holds it: the pid file is stale.
		return 0, false, syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	case errors.Is(err, syscall.EWOULDBLOCK):
		// The holder may not have written its pid yet; pid is then 0.
		pid, _ = readPID(pidFile)
		return pid, true, nil
	default:
		return 0, false, fmt.Errorf("lock %s: %w", pidFile, err)
	}
}

func readPID(pidFile string) (int, error) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// Service is a subcommand that runs in the background. The subcommand must
// call Acquire with the same PIDFile.
type Service struct {
	Name    string   // for messages, for example "server"
	PIDFile string   // locked by the running process
	LogFile string   // stdout and stderr of the process, appended
	Args    []string // arguments to Exe, for example {"daemon", "run"}
	Exe     string   // the binary; empty means this binary

	// Ready reports if the process serves. Start calls it until it returns
	// nil. Nil Ready means the lock alone is ready.
	Ready func(ctx context.Context) error
	// Wait bounds Start and Stop. Zero means 15 seconds.
	Wait time.Duration
}

const poll = 100 * time.Millisecond

func (s Service) wait() time.Duration {
	if s.Wait > 0 {
		return s.Wait
	}
	return 15 * time.Second
}

// Start starts the service in a new session, detached from the terminal,
// and waits until it is ready. If the process exits first, the error holds
// the end of its log.
func (s Service) Start(ctx context.Context) (pid int, err error) {
	if pid, ok, err := Running(s.PIDFile); err != nil {
		return 0, err
	} else if ok {
		return 0, &RunningError{Name: s.Name, PID: pid}
	}
	exe := s.Exe
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return 0, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.LogFile), 0o700); err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(s.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	start, _ := logf.Seek(0, io.SeekEnd)

	cmd := exec.Command(exe, s.Args...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid = cmd.Process.Pid
	// Wait reaps the child if it exits while we wait. If this process ends
	// first, the child keeps running: it is in its own session.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	ctx, cancel := context.WithTimeout(ctx, s.wait())
	defer cancel()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			return 0, fmt.Errorf("%s exited (%v):\n%s", s.Name, err, logSince(s.LogFile, start))
		case <-ctx.Done():
			return pid, fmt.Errorf("%s (pid %d) is not ready after %s; see %s", s.Name, pid, s.wait(), s.LogFile)
		case <-tick.C:
		}
		if got, ok, _ := Running(s.PIDFile); !ok || got != pid {
			continue
		}
		if s.Ready == nil || s.Ready(ctx) == nil {
			return pid, nil
		}
	}
}

// Stop sends SIGTERM to the service and waits until it ends.
func (s Service) Stop(ctx context.Context) error {
	pid, ok, err := Running(s.PIDFile)
	if err != nil {
		return err
	}
	if !ok || pid == 0 {
		return fmt.Errorf("%s: %w", s.Name, ErrNotRunning)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return s.WaitExit(ctx, pid)
}

// WaitExit waits until the process that held the lock as pid ends.
func (s Service) WaitExit(ctx context.Context, pid int) error {
	ctx, cancel := context.WithTimeout(ctx, s.wait())
	defer cancel()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		if got, ok, _ := Running(s.PIDFile); !ok || got != pid {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s (pid %d) still runs after %s; stop it with: kill -9 %d", s.Name, pid, s.wait(), pid)
		case <-tick.C:
		}
	}
}

// Tail copies the file at path to w. With follow, it keeps copying new
// data until ctx ends.
func Tail(ctx context.Context, path string, w io.Writer, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		if !follow {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// logSince returns the last lines that the log got after offset.
func logSince(path string, offset int64) string {
	b, err := os.ReadFile(path)
	if err != nil || offset > int64(len(b)) {
		return "(no log)"
	}
	b = bytes.TrimSpace(b[offset:])
	lines := bytes.Split(b, []byte("\n"))
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return string(bytes.Join(lines, []byte("\n")))
}

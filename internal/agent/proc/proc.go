// Package proc holds the process plumbing that all runtimes share: spawn an
// agent CLI in its own process group, stop it in steps, probe its version,
// and stamp IR events onto an unbounded event channel.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config describes a process to start.
type Config struct {
	Binary string
	Args   []string
	Dir    string
	Env    map[string]string // added to the parent environment
	Stderr io.Writer         // nil: kept only as a short tail
}

// Proc is one running agent process.
type Proc struct {
	cmd    *exec.Cmd
	Stdin  io.WriteCloser
	Stdout io.ReadCloser

	tail     *tailBuffer
	waitOnce sync.Once
	done     chan struct{}
	exitCode int
	waitErr  error
}

// Start starts the process in a new process group. The binary is found
// as Find finds it, and the PATH of the process gets what it may need
// (childEnv).
func Start(cfg Config) (*Proc, error) {
	bin, err := Find(cfg.Binary)
	if err != nil {
		return nil, fmt.Errorf("start %w", err)
	}
	cmd := exec.Command(bin, cfg.Args...)
	cmd.Dir = cfg.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = childEnv(cmd.Env, bin)
	tail := &tailBuffer{max: 8 << 10}
	if cfg.Stderr != nil {
		cmd.Stderr = io.MultiWriter(cfg.Stderr, tail)
	} else {
		cmd.Stderr = tail
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Binary, err)
	}
	return &Proc{cmd: cmd, Stdin: stdin, Stdout: stdout, tail: tail, done: make(chan struct{}), exitCode: -1}, nil
}

// Wait reaps the process. Call it once stdout returned EOF; os/exec closes
// the pipes in Wait. It is safe to call more than once.
func (p *Proc) Wait() (exitCode int, err error) {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		if p.cmd.ProcessState != nil {
			p.exitCode = p.cmd.ProcessState.ExitCode()
		}
		close(p.done)
	})
	return p.exitCode, p.waitErr
}

// Done is closed after Wait returned.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Pid returns the process ID.
func (p *Proc) Pid() int { return p.cmd.Process.Pid }

// StderrTail returns the last part of stderr, for error reports.
func (p *Proc) StderrTail() string { return strings.TrimSpace(p.tail.String()) }

// Signal sends sig to the whole process group.
func (p *Proc) Signal(sig syscall.Signal) {
	_ = syscall.Kill(-p.cmd.Process.Pid, sig)
}

// Stop closes stdin and waits up to grace for the process to exit. Then it
// sends SIGTERM to the group, and SIGKILL after another grace. The reader
// of stdout must keep running, or Done is never closed.
func (p *Proc) Stop(ctx context.Context, grace time.Duration) error {
	_ = p.Stdin.Close()
	for _, sig := range []syscall.Signal{0, syscall.SIGTERM, syscall.SIGKILL} {
		if sig != 0 {
			p.Signal(sig)
		}
		t := time.NewTimer(grace)
		select {
		case <-p.done:
			t.Stop()
			return nil
		case <-ctx.Done():
			t.Stop()
			p.Signal(syscall.SIGKILL)
			return ctx.Err()
		case <-t.C:
		}
	}
	return errors.New("proc: process did not exit after SIGKILL")
}

// Version runs "<binary> <args>" and returns the first line of its output.
func Version(ctx context.Context, binary string, args ...string) (string, error) {
	if args == nil {
		args = []string{"--version"}
	}
	if _, err := Find(binary); err != nil {
		return "", err
	}
	out, err := Output(ctx, binary, args...)
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if err != nil {
		if line != "" {
			return "", fmt.Errorf("%s: %w: %s", binary, err, line)
		}
		return "", fmt.Errorf("%s: %w", binary, err)
	}
	return line, nil
}

// Output runs "<binary> <args>", found as Start finds it, and returns its
// stdout and stderr.
func Output(ctx context.Context, binary string, args ...string) ([]byte, error) {
	bin, err := Find(binary)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = childEnv(os.Environ(), bin)
	return cmd.CombinedOutput()
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(b)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(b), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

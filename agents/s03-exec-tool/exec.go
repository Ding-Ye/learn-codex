package s03

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// ExecParams mirrors codex-rs/core/src/exec.rs's ExecParams.
type ExecParams struct {
	Command        []string
	Cwd            string
	Env            map[string]string
	Timeout        time.Duration // 0 == no timeout
	OutputCapBytes int           // default 256 << 10 (256 KB)
}

// ExecResult is the synchronous summary returned to the caller after the
// streaming events have all been emitted.
type ExecResult struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
	Err       error
}

// Stream is the type the caller uses to discriminate output deltas.
type Stream int

const (
	StreamStdout Stream = iota
	StreamStderr
)

// OutputDelta is one streamed chunk of output. Sequence is per-stream.
type OutputDelta struct {
	Stream Stream
	Bytes  []byte
	Seq    int
}

const (
	defaultOutputCap = 256 << 10
	maxDeltas        = 10_000 // upstream's hard cap on per-turn deltas
)

// Run spawns the command, reads stdout/stderr concurrently, sends each chunk
// over `deltas` (caller may pass nil to suppress), enforces the timeout and
// the output cap. Returns the final ExecResult after the process exits.
func Run(ctx context.Context, p ExecParams, deltas chan<- OutputDelta) ExecResult {
	if p.OutputCapBytes <= 0 {
		p.OutputCapBytes = defaultOutputCap
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}

	if len(p.Command) == 0 {
		return ExecResult{ExitCode: -1, Err: fmt.Errorf("empty command")}
	}

	cmd := exec.CommandContext(ctx, p.Command[0], p.Command[1:]...)
	cmd.Dir = p.Cwd
	if p.Env != nil {
		envSlice := make([]string, 0, len(p.Env))
		for k, v := range p.Env {
			envSlice = append(envSlice, k+"="+v)
		}
		cmd.Env = envSlice
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{ExitCode: -1, Err: err}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return ExecResult{ExitCode: -1, Err: err}
	}

	if err := cmd.Start(); err != nil {
		return ExecResult{ExitCode: -1, Err: err}
	}

	var (
		stdoutBuf, stderrBuf bytes.Buffer
		wg                   sync.WaitGroup
		mu                   sync.Mutex
		written              int
		truncated            bool
		seq                  int
	)
	wg.Add(2)

	pump := func(s Stream, r io.Reader, buf *bytes.Buffer) {
		defer wg.Done()
		chunk := make([]byte, 4096)
		for {
			n, err := r.Read(chunk)
			if n > 0 {
				mu.Lock()
				if written+n > p.OutputCapBytes {
					take := p.OutputCapBytes - written
					if take > 0 {
						buf.Write(chunk[:take])
						written += take
						if deltas != nil && seq < maxDeltas {
							seq++
							deltas <- OutputDelta{Stream: s, Bytes: append([]byte{}, chunk[:take]...), Seq: seq}
						}
					}
					truncated = true
					mu.Unlock()
					_, _ = io.Copy(io.Discard, r) // drain the rest so the process can exit
					return
				}
				buf.Write(chunk[:n])
				written += n
				if deltas != nil && seq < maxDeltas {
					seq++
					deltas <- OutputDelta{Stream: s, Bytes: append([]byte{}, chunk[:n]...), Seq: seq}
				}
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}
	go pump(StreamStdout, stdoutPipe, &stdoutBuf)
	go pump(StreamStderr, stderrPipe, &stderrBuf)
	wg.Wait()

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}

	res := ExecResult{
		ExitCode:  exitCode,
		Stdout:    stdoutBuf.Bytes(),
		Stderr:    stderrBuf.Bytes(),
		Truncated: truncated,
	}
	if waitErr != nil && ctx.Err() != nil {
		res.Err = ctx.Err() // timeout / cancellation surfaces here
	}
	return res
}

package session

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// CaptureEnv is set in the watched shell when its output is being captured;
// the shell hooks only print markers then.
const CaptureEnv = "MEM_CAPTURE"

// outputMark prefixes the invisible markers the shell hooks print around
// each command: start before it runs, end (or skip, when the hook decided
// not to record the command) after it. The recorder strips them from the
// stream and keeps what lies between.
const outputMark = "\x1b]777;mem;"

// recordMark prefixes output records in a session file. It follows the
// record of the command that produced the output.
const recordMark = '\x01'

// Limits for the raw bytes held for one command, before cleaning.
const (
	rawHead = 64 << 10
	rawTail = 192 << 10
)

// Recorder is an io.Writer that passes terminal output through unchanged
// (minus the markers) and appends the output of each recorded command to a
// session file.
type Recorder struct {
	out  io.Writer
	path string

	carry     []byte // possible start of a marker split across writes
	capturing bool
	head      []byte
	tail      []byte
	dropped   bool // the middle of a very long output was discarded
	altScreen bool // a full-screen program ran; its output is not text
}

// NewRecorder returns a Recorder forwarding to out and appending outputs to
// the session file at path.
func NewRecorder(out io.Writer, path string) *Recorder {
	return &Recorder{out: out, path: path}
}

// Write implements io.Writer. It reports len(p) on success even though
// markers are not forwarded.
func (r *Recorder) Write(p []byte) (int, error) {
	data := p
	if len(r.carry) > 0 {
		data = append(r.carry, p...)
		r.carry = nil
	}

	for len(data) > 0 {
		i := bytes.Index(data, []byte(outputMark))
		if i < 0 {
			// Hold back a tail that may be the beginning of a marker.
			keep := partialMark(data)
			if err := r.emit(data[:len(data)-keep]); err != nil {
				return 0, err
			}
			r.carry = append([]byte(nil), data[len(data)-keep:]...)
			return len(p), nil
		}
		if err := r.emit(data[:i]); err != nil {
			return 0, err
		}
		data = data[i+len(outputMark):]

		end := bytes.IndexByte(data, '\a')
		if end < 0 {
			if len(data) > 16 { // not one of ours
				if err := r.emit([]byte(outputMark)); err != nil {
					return 0, err
				}
				continue
			}
			r.carry = append([]byte(outputMark), data...)
			return len(p), nil
		}
		r.mark(string(data[:end]))
		data = data[end+1:]
	}
	return len(p), nil
}

// partialMark returns how many bytes at the end of data are a proper prefix
// of outputMark.
func partialMark(data []byte) int {
	for n := min(len(outputMark)-1, len(data)); n > 0; n-- {
		if bytes.HasSuffix(data, []byte(outputMark[:n])) {
			return n
		}
	}
	return 0
}

// emit forwards visible bytes and, while a command runs, keeps a copy.
func (r *Recorder) emit(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if r.capturing {
		r.collect(b)
	}
	_, err := r.out.Write(b)
	return err
}

func (r *Recorder) collect(b []byte) {
	if bytes.Contains(b, []byte("\x1b[?1049h")) || bytes.Contains(b, []byte("\x1b[?47h")) {
		r.altScreen = true
	}
	if room := rawHead - len(r.head); room > 0 && len(r.tail) == 0 {
		n := min(room, len(b))
		r.head = append(r.head, b[:n]...)
		b = b[n:]
	}
	r.tail = append(r.tail, b...)
	if len(r.tail) > 2*rawTail {
		r.tail = append([]byte(nil), r.tail[len(r.tail)-rawTail:]...)
		r.dropped = true
	}
}

func (r *Recorder) mark(kind string) {
	switch kind {
	case "start":
		r.capturing = true
		r.head, r.tail, r.dropped, r.altScreen = nil, nil, false, false
	case "end":
		if r.capturing && !r.altScreen {
			r.flush()
		}
		r.capturing = false
	default: // "skip" and anything unknown
		r.capturing = false
	}
}

// flush writes the collected output of the finished command to the session
// file. Failures are ignored: losing an output must not disturb the shell.
func (r *Recorder) flush() {
	raw := string(r.head) + string(r.tail)
	if r.dropped {
		raw = string(r.head) + "\n…\n" + string(r.tail)
	}
	out := ProcessOutput(raw)
	if out == "" {
		return
	}
	// O_APPEND: `mem remember` may truncate the file between commands.
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%c%s\x00", recordMark, out)
}

// RunCaptured starts the shell on a pseudo-terminal wired to in and out (usually
// the user's terminal) and records the output of each command into
// sessionPath. It returns when the shell exits. in must be a terminal.
func (s *Shell) RunCaptured(in *os.File, out io.Writer, sessionPath string) error {
	s.cmd.Env = append(s.cmd.Env, CaptureEnv+"=1")

	ptmx, err := pty.Start(s.cmd)
	if err != nil {
		return fmt.Errorf("starting shell on a pty: %w", err)
	}
	defer ptmx.Close()

	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	go func() {
		for range resize {
			_ = pty.InheritSize(in, ptmx)
		}
	}()
	resize <- syscall.SIGWINCH

	// The pty's line discipline handles Ctrl-C, Ctrl-Z and echo; mem only
	// shuttles bytes.
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("setting terminal to raw mode: %w", err)
	}
	defer term.Restore(int(in.Fd()), state)

	go func() { _, _ = io.Copy(ptmx, in) }()
	_, _ = io.Copy(NewRecorder(out, sessionPath), ptmx) // ends with EIO when the shell exits

	return s.cmd.Wait()
}

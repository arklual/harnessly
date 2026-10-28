package agentlogin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Codex can run a device-code login for a client: `codex app-server` speaks JSON-RPC, one message per line, on its
// standard streams. The client starts the login, shows the address and the code, and the server tells when the person
// has confirmed. The process has to stay up for that, so ending it is also how a login is cancelled. The protocol is
// marked experimental by Codex, so a login that cannot be done this way falls back to reading the screen of
// `codex login --device-auth`.

// rpcMessage is a line of the protocol: a response (id, result or error) or a notification (method).
type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// appServer is a running `codex app-server`.
type appServer struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	msgs  chan rpcMessage // closed when the process's output ends
	nextN int

	exited chan struct{}
	quit   chan struct{} // closed by stop, so the reader never waits for a consumer that has gone
}

// startAppServer runs cmd (the login shell command that runs `codex app-server`) with pipes on its standard streams.
func startAppServer(cmd *exec.Cmd) (*appServer, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// The output is a pipe of our own, not StdoutPipe: Wait closes that one at the process's exit, and a last message
	// written just before it would be lost.
	out, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = w
	// Its warnings (a folder it cannot make helpers in) go to stderr; they are of no use here.
	cmd.Stderr = nil
	err = cmd.Start()
	_ = w.Close()
	if err != nil {
		_ = out.Close()
		return nil, err
	}
	s := &appServer{cmd: cmd, stdin: in, msgs: make(chan rpcMessage, 16), exited: make(chan struct{}), quit: make(chan struct{})}
	go func() {
		defer close(s.msgs)
		defer out.Close()
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			var m rpcMessage
			// A line that is not a message (a banner on the wrong stream) is skipped.
			if json.Unmarshal(sc.Bytes(), &m) != nil || (m.ID == nil && m.Method == "") {
				continue
			}
			select {
			case s.msgs <- m:
			case <-s.quit:
				return
			}
		}
	}()
	go func() {
		_ = cmd.Wait()
		close(s.exited)
	}()
	return s, nil
}

func (s *appServer) write(m map[string]any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.stdin.Write(append(b, '\n'))
	return err
}

// notify writes a notification: a message nobody answers.
func (s *appServer) notify(method string) error { return s.write(map[string]any{"method": method}) }

// request writes a request and returns its id.
func (s *appServer) request(method string, params any) (int, error) {
	s.nextN++
	return s.nextN, s.write(map[string]any{"id": s.nextN, "method": method, "params": params})
}

// call sends a request and waits for its answer; notifications met on the way are given to onNote.
func (s *appServer) call(ctx context.Context, method string, params any, onNote func(rpcMessage)) (json.RawMessage, error) {
	id, err := s.request(method, params)
	if err != nil {
		return nil, err
	}
	return s.await(ctx, id, onNote)
}

// await waits for the answer to request id.
func (s *appServer) await(ctx context.Context, id int, onNote func(rpcMessage)) (json.RawMessage, error) {
	want := fmt.Sprint(id)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case m, ok := <-s.msgs:
			if !ok {
				return nil, errors.New("codex app-server ended")
			}
			if m.Method != "" {
				if onNote != nil {
					onNote(m)
				}
				continue
			}
			if string(m.ID) != want {
				continue
			}
			if m.Error != nil {
				return nil, errors.New(m.Error.Message)
			}
			return m.Result, nil
		}
	}
}

// stop ends the process; its output and exit are drained by the goroutines.
func (s *appServer) stop() {
	close(s.quit)
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-s.exited:
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.exited
	}
}

// deviceLogin is what `account/login/start` answers for a device-code login.
type deviceLogin struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId"`
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
}

// loginCompleted is the `account/login/completed` notification.
type loginCompleted struct {
	Success bool    `json:"success"`
	LoginID *string `json:"loginId"`
	Error   *string `json:"error"`
}

// Package connect lets an agent on the runtime's machine run commands on another device — a person's laptop — that
// dials the runtime itself (Harnessly Connect). The device holds the processes: a link that drops and comes back
// finds them still running, and the output they wrote meanwhile is sent again from the offset the runtime has.
//
// Three parties:
//   - the device (Device, cmd/connect) dials wss://<runtime>/connect/v1/dial, signed with its Ed25519 key;
//   - the runtime (Server) keeps the devices, the live links and the running commands;
//   - the wrapper (RunBash, cmd/connect-bash) is the agent's shell on the runtime's machine (CLAUDE_CODE_SHELL): it
//     opens /api/cli/connect/exec and becomes the command running on the device.
package connect

import (
	"encoding/binary"
	"errors"
)

// Protocol is the version a device and the runtime speak; a mismatch is refused at the dial.
const Protocol = 1

// Stream kinds of a binary data frame.
const (
	KindStdout byte = 1
	KindStderr byte = 2
)

// Msg is every JSON frame of both links (device ↔ runtime, wrapper ↔ runtime); T says which fields mean something.
type Msg struct {
	T   string `json:"t"`
	SID string `json:"sid,omitempty"`

	// exec (wrapper → runtime → device)
	Device   string            `json:"device,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Cwd      string            `json:"cwd,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	FilesIn  []File            `json:"filesIn,omitempty"`
	FilesOut []string          `json:"filesOut,omitempty"`

	// attach (runtime → device): the bytes of each stream the runtime already has.
	Out int64 `json:"out,omitempty"`
	Err int64 `json:"err,omitempty"`

	// stdin (wrapper → runtime → device); EOF closes it.
	Data []byte `json:"data,omitempty"`
	EOF  bool   `json:"eof,omitempty"`

	// signal: INT, TERM, HUP or KILL.
	Sig string `json:"sig,omitempty"`

	// exit (device → runtime → wrapper)
	Code  int    `json:"code"`
	Files []File `json:"files,omitempty"`

	// hello (device → runtime): who it is and the commands it still holds.
	Name    string   `json:"name,omitempty"`
	OS      string   `json:"os,omitempty"`
	Arch    string   `json:"arch,omitempty"`
	Version string   `json:"version,omitempty"`
	Proto   int      `json:"proto,omitempty"`
	SIDs    []string `json:"sids,omitempty"`

	// error: a refusal the wrapper prints and exits on.
	Error string `json:"error,omitempty"`
}

// File is a small file carried with a command: the agent's own bookkeeping (its shell snapshot, the file its shell
// writes the working directory into) lives on the runtime's machine and is mirrored around the command.
type File struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

// TmpToken in a command's arguments and file paths stands for the device's own folder for such files; the device puts
// its path in its own shell's form (on Windows /c/Users/…).
const TmpToken = "@CONNECT_TMP@"

const dataHeader = 1 + 8 + 8

// EncodeData is a device's data frame: kind, the command's number, the offset of the payload in its stream.
func EncodeData(kind byte, sid uint64, off int64, payload []byte) []byte {
	b := make([]byte, dataHeader+len(payload))
	b[0] = kind
	binary.BigEndian.PutUint64(b[1:9], sid)
	binary.BigEndian.PutUint64(b[9:17], uint64(off))
	copy(b[dataHeader:], payload)
	return b
}

var errShortFrame = errors.New("connect: short data frame")

// DecodeData reads a device's data frame.
func DecodeData(b []byte) (kind byte, sid uint64, off int64, payload []byte, err error) {
	if len(b) < dataHeader {
		return 0, 0, 0, nil, errShortFrame
	}
	return b[0], binary.BigEndian.Uint64(b[1:9]), int64(binary.BigEndian.Uint64(b[9:17])), b[dataHeader:], nil
}

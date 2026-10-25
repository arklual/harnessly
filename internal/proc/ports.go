package proc

import (
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
)

// listenState is the TCP state of a listening socket in /proc/net/tcp*.
const listenState = "0A"

// listeningSockets maps the inode of every listening TCP socket on the machine to its local port.
func listeningSockets() map[string]int {
	out := map[string]int{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines[min(1, len(lines)):] {
			f := strings.Fields(line)
			// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode
			if len(f) < 10 || f[3] != listenState {
				continue
			}
			i := strings.LastIndexByte(f[1], ':')
			if i < 0 {
				continue
			}
			port, err := strconv.ParseInt(f[1][i+1:], 16, 32)
			if err != nil {
				continue
			}
			out[f[9]] = int(port)
		}
	}
	return out
}

// ListeningPorts are the TCP ports the given processes listen on, ascending and without repeats: what `ss -tlnp` lists
// for them. Sockets of processes this user cannot look into are not seen, as with ss.
func ListeningPorts(pids []int) []int {
	if len(pids) == 0 {
		return []int{}
	}
	sockets := listeningSockets()
	if len(sockets) == 0 {
		return []int{}
	}
	seen := map[int]bool{}
	out := []int{}
	for _, pid := range pids {
		fdDir := dir(pid) + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(target, "socket:[")
			if !ok {
				continue
			}
			if port, ok := sockets[strings.TrimSuffix(inode, "]")]; ok && !seen[port] {
				seen[port] = true
				out = append(out, port)
			}
		}
	}
	slices.Sort(out)
	return out
}

// TreeListeningPorts are the ports listened on by the processes below root (a pane's shell): a dev server started
// from the shell, however deep.
func TreeListeningPorts(root int) []int {
	return ListeningPorts(Descendants(root))
}

// SocketInode is the inode of the TCP socket on this machine whose own end is client and whose other end is server
// (any state but listening): the client's end of a loopback connection a server took at server. The rows are matched
// on both whole addresses, an IPv4 one also in its v4-mapped form in tcp6. False when no row or more than one matches.
func SocketInode(client, server netip.AddrPort) (string, bool) {
	inode, found := "", 0
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines[min(1, len(lines)):] {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] == listenState || !sameEnd(f[1], client) || !sameEnd(f[2], server) {
				continue
			}
			inode, found = f[9], found+1
		}
	}
	return inode, found == 1 && inode != "0"
}

// sameEnd tells whether a /proc/net/tcp* address (hex address in the kernel's byte order per 32-bit word, colon, hex
// port) is want.
func sameEnd(field string, want netip.AddrPort) bool {
	addr, port, ok := strings.Cut(field, ":")
	if !ok || hexPort(port) != int(want.Port()) {
		return false
	}
	b, err := hex.DecodeString(addr)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return false
	}
	for i := 0; i < len(b); i += 4 {
		binary.BigEndian.PutUint32(b[i:], binary.NativeEndian.Uint32(b[i:]))
	}
	got, _ := netip.AddrFromSlice(b)
	return got.Unmap() == want.Addr().Unmap()
}

func hexPort(port string) int {
	n, err := strconv.ParseInt(port, 16, 32)
	if err != nil {
		return -1
	}
	return int(n)
}

// HoldsSocket tells whether one of the process's open files is the socket of this inode.
func HoldsSocket(pid int, inode string) bool {
	fdDir := dir(pid) + "/fd"
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}
	want := "socket:[" + inode + "]"
	for _, fd := range fds {
		if target, err := os.Readlink(fdDir + "/" + fd.Name()); err == nil && target == want {
			return true
		}
	}
	return false
}

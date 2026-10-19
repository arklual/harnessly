package proc

import (
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

package ports

import "testing"

func TestDevServersLeaveThePlatformAndOtherUsersOut(t *testing.T) {
	out := `LISTEN 0 511 127.0.0.1:5173 0.0.0.0:* users:(("node",pid=10,fd=20))
LISTEN 0 511 *:8022 *:* users:(("runtime",pid=11,fd=3))
LISTEN 0 511 127.0.0.1:5432 0.0.0.0:*
LISTEN 0 511 [::1]:3000 [::]:* users:(("next-server",pid=12,fd=21))
LISTEN 0 511 127.0.0.1:4000 0.0.0.0:* users:(("vite",pid=13,fd=22))
LISTEN 0 511 127.0.0.1:9000 0.0.0.0:* users:(("python3",pid=14,fd=5))
LISTEN 0 511 127.0.0.1:8080 0.0.0.0:* users:(("node",pid=15,fd=5))
LISTEN 0 511 [::1]:8080 [::]:* users:(("runtime",pid=11,fd=9))
LISTEN 0 511 127.0.0.1:8081 0.0.0.0:* users:(("node",pid=16,fd=5))
LISTEN 0 511 127.0.0.2:8081 0.0.0.0:*`
	got := DevServers(out, func(pid int) bool { return pid == 13 }, map[int]bool{9000: true})
	if len(got) != 2 || !got[5173] || !got[3000] {
		t.Fatalf("dev servers: %v", got)
	}
}

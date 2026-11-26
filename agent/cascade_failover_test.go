package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCascadeFailover(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a Unix shell")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	const ipv4 = "192.0.2.1:443"
	const ipv6 = "[2001:db8::1]:443"
	for _, tc := range []struct {
		name, initial, working, want string
		fails                        bool
	}{
		{"healthy IPv4", ipv4, ipv4, ipv4, false},
		{"IPv4 outage", ipv4, ipv6, ipv6, false},
		{"healthy fallback", ipv6, ipv6, ipv6, false},
		{"IPv6 outage", ipv6, ipv4, ipv4, false},
		{"both down", ipv4, "", ipv6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write("endpoint", tc.initial)
			write("ping", "#!/bin/sh\n[ \"$(cat \"$TEST_ENDPOINT\")\" = \"$TEST_WORKING\" ]\n")
			write("awg", `#!/bin/sh
case "$1" in
  show) printf 'peer-key\t%s\n' "$(cat "$TEST_ENDPOINT")" ;;
  set) [ "$2:$3:$4:$5" = awg0:peer:peer-key:endpoint ] && printf '%s' "$6" > "$TEST_ENDPOINT" ;;
  *) exit 2 ;;
esac
`)
			script := Files("cascade-awg0")["vpnkit-cascade-failover.sh"]
			cmd := exec.Command(bash, "-s", "--", "peer-key", "10.100.0.1", ipv4, ipv6)
			cmd.Stdin = strings.NewReader(script)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TEST_ENDPOINT="+filepath.Join(dir, "endpoint"), "TEST_WORKING="+tc.working)
			if out, err := cmd.CombinedOutput(); (err != nil) != tc.fails {
				t.Fatalf("unexpected result: %v\n%s", err, out)
			}
			got, err := os.ReadFile(filepath.Join(dir, "endpoint"))
			if err != nil || string(got) != tc.want {
				t.Fatalf("endpoint = %q, want %q (read error: %v)", got, tc.want, err)
			}
		})
	}
}

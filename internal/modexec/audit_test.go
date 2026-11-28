package modexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfigAudit(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	const module = "client-awg"
	const run = "/var/lib/vpnkit/runs/client-awg-20261128-120000"
	const config = "/etc/amnezia/amneziawg/awg2.conf"
	const firewall = "/etc/nftables.d/30-vpnkit-client-awg2.nft"
	dir := t.TempDir()
	execute := func(script string) string {
		t.Helper()
		script = strings.NewReplacer("/etc/", dir+"/etc/", "/var/lib/vpnkit/", dir+"/var/lib/vpnkit/").Replace(script)
		cmd := exec.Command(sh, "-s")
		cmd.Stdin = strings.NewReader("set -e\n" + script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("audit script: %v\n%s", err, out)
		}
		return string(out)
	}
	write := func(name, value string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(firewall, "firewall")
	write("/etc/dnsmasq.d/30-vpnkit-awg2.conf", "dns")
	write("/var/lib/vpnkit/modules/client-awg", "installed=2026-11-28 run="+dir+run+" code=1234\n")
	write(config, "[Interface]\nMTU = 1260\nPrivateKey = sample\n")
	check := func() ConfigAudit {
		return parseAudit(execute(configAuditScript([]string{module}, "entry", "")))[module]
	}
	if got := check(); got.Baseline || len(got.Issues) != 0 {
		t.Fatalf("legacy: %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, run), 0o700); err != nil {
		t.Fatal(err)
	}
	execute(recordConfig(module, run, nil))
	write(config, "[Interface]\nMTU = 1260\nPrivateKey = sample\n\n# name: test\n[Peer]\nPublicKey = test\n")
	if got := check(); !got.Baseline || len(got.Issues) != 0 {
		t.Fatalf("after adding a peer: %+v", got)
	}
	write(config, "[Interface]\nMTU = 1240\nPrivateKey = sample\n")
	if err := os.Remove(filepath.Join(dir, firewall)); err != nil {
		t.Fatal(err)
	}
	got := check()
	if !got.Baseline || !slices.Contains(got.Issues, "Изменён: "+dir+config) ||
		!slices.Contains(got.Issues, "Отсутствует: "+dir+firewall) {
		t.Fatalf("after changing and deleting files: %+v", got)
	}
}

func TestParseConfigAudit(t *testing.T) {
	got := parseAudit("subscriptions\tbaseline\nsubscriptions\tchanged\t/etc/vpnkit/sub.env\nsubscriptions\tchanged\t/etc/vpnkit/sub.env\n")
	s := got["subscriptions"]
	if !s.Baseline || !slices.Equal(s.Issues, []string{"Изменён: /etc/vpnkit/sub.env"}) {
		t.Fatalf("unexpected audit: %+v", got)
	}
}

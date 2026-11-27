package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

func TestVerifyClientAWGFirewall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KROT_HOME", dir)
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "age.key"), []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const dns = "etc/dnsmasq.d/30-vpnkit-awg2.conf"
	for _, tc := range []struct {
		name    string
		files   []string
		missing []string
	}{
		{"current firewall name", []string{"etc/nftables.d/30-vpnkit-client-awg2.nft", dns}, nil},
		{"legacy firewall name", []string{"etc/nftables.d/30-krot-client-awg2.nft", dns}, nil},
		{"unrelated firewall file", []string{"etc/nftables.d/30-vpnkit-other.nft", dns}, []string{clientFirewallPattern}},
		{"missing DNS file", []string{"etc/nftables.d/30-vpnkit-client-awg2.nft"}, []string{"etc/dnsmasq.d/30-vpnkit-*.conf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var encrypted bytes.Buffer
			enc, err := age.Encrypt(&encrypted, id.Recipient())
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(enc)
			archive := tar.NewWriter(gz)
			files := append([]string{markerDir + "client-awg"}, tc.files...)
			for _, name := range files {
				if err := archive.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600}); err != nil {
					t.Fatal(err)
				}
			}
			for _, writer := range []io.Closer{archive, gz, enc} {
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(t.TempDir(), "test.tar.gz.age")
			if err := os.WriteFile(file, encrypted.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := Verify(file, "entry")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(report.Missing, tc.missing) {
				t.Errorf("missing = %v, want %v", report.Missing, tc.missing)
			}
		})
	}
}

func TestCreateBackupScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("backup service uses Linux shell utilities")
	}

	for _, tc := range []struct {
		name   string
		action string
		want   string
	}{
		{"new archive", `touch "$BACKUP_DIR/node-2.tar.gz.age"`, "node-2.tar.gz.age"},
		{"new archive with the same timestamped name", `touch "$BACKUP_DIR/new"
mv "$BACKUP_DIR/new" "$BACKUP_DIR/node-1.tar.gz.age"`, "node-1.tar.gz.age"},
		{"retry after joining an existing run", `if [ -e "$BACKUP_DIR/joined" ]; then
    touch "$BACKUP_DIR/node-2.tar.gz.age"
else
    touch "$BACKUP_DIR/joined"
fi`, "node-2.tar.gz.age"},
		{"service returns without a new archive", "return 0", ""},
		{"service fails", "return 1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old := filepath.Join(dir, "node-1.tar.gz.age")
			if err := os.WriteFile(old, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			earlier := time.Unix(1, 0)
			if err := os.Chtimes(old, earlier, earlier); err != nil {
				t.Fatal(err)
			}
			// Stub systemctl while keeping the actual shell code and filesystem commands.
			script := "systemctl() {\n" + tc.action + "\n}\n" +
				strings.ReplaceAll(createBackupScript, "/var/backups/vpnkit", `"$BACKUP_DIR"`)
			cmd := exec.Command("sh", "-s")
			cmd.Env = append(os.Environ(), "BACKUP_DIR="+dir)
			cmd.Stdin = strings.NewReader(script)
			out, err := cmd.CombinedOutput()
			if tc.want == "" {
				if err == nil {
					t.Fatalf("reported success with no new archive: %s", out)
				}
				return
			}
			if err != nil || strings.TrimSpace(string(out)) != filepath.Join(dir, tc.want) {
				t.Fatalf("script returned %q, %v", out, err)
			}
		})
	}
}

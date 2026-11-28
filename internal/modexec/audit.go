package modexec

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"github.com/xDarkOne/krot/agent"
	"github.com/xDarkOne/krot/internal/remote"
)

type moduleFiles struct {
	config, optional []string
	embedded         map[string]string // deployed path → embedded filename
}

// Client registries, generated feeds, certificates and editable site content
// are excluded. These paths belong to the module installer.
var auditedFiles = map[string]moduleFiles{
	"amneziawg": {config: []string{"/usr/bin/awg", "/usr/bin/awg-quick"}},
	"base-host": {config: []string{"/etc/nftables.conf", "/etc/nftables.d/10-vpnkit-fw.nft", "/etc/sysctl.d/99-vpnkit.conf", "/etc/fail2ban/jail.d/vpnkit.conf"}},
	"backup":    {config: []string{"/etc/vpnkit/backup.conf", "/etc/systemd/system/vpnkit-backup.timer"}, embedded: map[string]string{"/usr/local/sbin/vpnkit-backup.sh": "vpnkit-backup.sh"}},
	"cascade-awg0": {config: []string{"/etc/amnezia/amneziawg/awg0.conf", "/etc/nftables.d/20-vpnkit-cascade.nft"},
		optional: []string{"/etc/systemd/system/vpnkit-cascade-failover.service", "/etc/systemd/system/vpnkit-cascade-failover.timer"}},
	"client-awg": {config: []string{"/etc/nftables.d/30-vpnkit-client-*.nft", "/etc/dnsmasq.d/30-vpnkit-*.conf"}},
	"hysteria2":  {config: []string{"/etc/vpnkit/hy2.env", "/etc/nftables.d/46-vpnkit-hy2.nft", "/etc/systemd/system/sing-box.service.d/10-vpnkit-single-config.conf"}},
	"monitoring": {config: []string{"/etc/vpnkit/watch.conf", "/etc/systemd/system/vpnkit-watch.timer"}, embedded: map[string]string{"/usr/local/sbin/vpnkit-watch.sh": "vpnkit-watch.sh"}},
	"mtproxy":    {config: []string{"/etc/vpnkit/mtproxy.env"}, optional: []string{"/etc/nftables.d/47-vpnkit-mtproxy.nft"}},
	"nginx-tls": {config: []string{"/etc/vpnkit/nginx.env", "/etc/nginx/sites-available/vpnkit-*", "/etc/nftables.d/40-vpnkit-nginx.nft"},
		optional: []string{"/etc/systemd/system/certbot.service.d/50-vpnkit-cascade.conf", "/etc/nginx/modules-enabled/99-vpnkit-stream.conf"}},
	"relocate":      {config: []string{"/etc/vpnkit/relocate.conf"}, embedded: map[string]string{"/usr/local/sbin/relocate": "relocate", "/usr/local/sbin/vpnkit-reip": "vpnkit-reip"}},
	"split-routing": {config: []string{"/etc/dnsmasq.d/10-vpnkit-split.conf", "/etc/nftables.d/25-vpnkit-split.nft", "/etc/vpnkit/split.conf"}, embedded: map[string]string{"/usr/local/sbin/vpnkit-ru-domains.sh": "vpnkit-ru-domains.sh", "/usr/local/sbin/vpnkit-ru-prune.py": "vpnkit-ru-prune.py"}},
	"subscriptions": {config: []string{"/etc/vpnkit/sub.env", "/etc/nginx/vpnkit.d/*-site-sub.conf"}},
	"traffic-guard": {config: []string{"/etc/systemd/system/traffic-guard.service", "/etc/systemd/system/traffic-guard.timer"}, embedded: map[string]string{"/usr/local/sbin/traffic-guard": "traffic-guard"}},
	"vpn-bot":       {config: []string{"/etc/systemd/system/vpn-bot.service", "/etc/systemd/system/vpn-stats.timer"}, embedded: map[string]string{"/usr/local/sbin/vpn-bot.py": "vpn-bot.py", "/usr/local/sbin/vpn-stats.py": "vpn-stats.py"}},
	"warp-ru":       {config: []string{"/etc/systemd/system/warp-ru.service"}, embedded: map[string]string{"/usr/local/sbin/warp-ru": "warp-ru"}},
	"xray":          {config: []string{"/etc/vpnkit/xray.env", "/etc/nftables.d/45-vpnkit-xray.nft", "/etc/nginx/vpnkit.d/*-xhttp-xray.conf", "/etc/systemd/system/xray.service"}},
}

// Client edits add peer sections to the same file. The installer owns its
// Interface directives, independent of the registry and its comments.
const auditChecksum = `checksum() {
  case "$module:$1" in
    client-awg:/etc/amnezia/amneziawg/*.conf)
      config=$(awk '/^\[/{iface=($0=="[Interface]")} iface && /^[[:space:]]*[[:alnum:]]+[[:space:]]*=/{print}' "$1") || return 1
      printf '%s\n' "$config" | sha256sum ;;
    *) sha256sum -- "$1" ;;
  esac
}
`

func clientConfigPath(iface string) string {
	if iface == "" {
		iface = "awg2"
	}
	return "/etc/amnezia/amneziawg/" + iface + ".conf"
}

// Bind a private checksum snapshot to the successful install's run.
func recordConfig(module, run string, params map[string]string) string {
	f := auditedFiles[module]
	paths := append(slices.Clone(f.config), f.optional...)
	if module == "client-awg" {
		paths = append(paths, shq(clientConfigPath(params["IFACE"])))
	}
	if len(paths) == 0 {
		return ""
	}
	return auditChecksum + fmt.Sprintf(`(
  umask 077
  module=%s
  for file in %s; do
    if [ -f "$file" ]; then
      sum=$(checksum "$file") || exit 1
      printf '%%s  %%s\n' "${sum%%%% *}" "$file"
    fi
  done > %s
) || { rm -f %s; echo "  • контрольные суммы конфигурации не сохранены" >&2; }
`, shq(module), strings.Join(paths, " "), shq(run+"/audit.sha256"), shq(run+"/audit.sha256"))
}

type ConfigAudit struct {
	Baseline bool
	Issues   []string
}

func configAuditScript(names []string, role, iface string) string {
	var script strings.Builder
	script.WriteString(auditChecksum)
	script.WriteString(`check_file() {
  if [ ! -f "$1" ]; then
    printf '%s\tmissing\t%s\n' "$module" "$1"
    return
  fi
  got=$(checksum "$1" 2>/dev/null) || { printf '%s\tunreadable\t%s\n' "$module" "$1"; return; }
  if [ "${got%% *}" != "$2" ]; then printf '%s\tchanged\t%s\n' "$module" "$1"; fi
}
`)
	for _, name := range names {
		f, ok := auditedFiles[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&script, "module=%s\n", shq(name))
		script.WriteString(`mark=/var/lib/vpnkit/modules/$module
[ -f "$mark" ] || mark=/etc/vpnkit/modules/$module
run=$(sed -n 's/.* run=\([^ ]*\).*/\1/p' "$mark" 2>/dev/null)
prefix=/var/lib/vpnkit/runs/$module-
case "$run" in
  "$prefix"*)
    suffix=${run#"$prefix"}
    case "$suffix" in
      ''|*[!0-9-]*) ;;
      *)
        if [ -s "$run/audit.sha256" ]; then
          printf '%s\tbaseline\n' "$module"
          while read -r hash file; do check_file "$file" "$hash"; done < "$run/audit.sha256"
        fi ;;
    esac ;;
esac
`)
		for _, path := range f.config {
			fmt.Fprintf(&script, "found=; for file in %s; do [ ! -f \"$file\" ] || found=1; done\n[ -n \"$found\" ] || printf '%%s\\tmissing\\t%%s\\n' \"$module\" %s\n", path, shq(path))
		}
		if name == "client-awg" {
			fmt.Fprintf(&script, "[ -f %[1]s ] || printf '%%s\\tmissing\\t%%s\\n' \"$module\" %[1]s\n", shq(clientConfigPath(iface)))
		}
		embedded := agent.Files(name)
		check := func(path, source string) {
			body := strings.TrimRight(embedded[source], "\n") + "\n"
			fmt.Fprintf(&script, "check_file %s %x\n", shq(path), sha256.Sum256([]byte(body)))
		}
		for path, source := range f.embedded {
			check(path, source)
		}
		switch name {
		case "xray", "mtproxy", "nginx-tls", "subscriptions":
			check("/usr/local/sbin/vpnkit-gen.py", "vpnkit-gen.py")
		}
		if name == "nginx-tls" && role == "entry" {
			check("/usr/local/sbin/vpnkit-certbot", "vpnkit-certbot.py")
		}
		if name == "cascade-awg0" && role == "entry" {
			script.WriteString("if [ -f /etc/systemd/system/vpnkit-cascade-failover.timer ]; then\n")
			check("/usr/local/sbin/vpnkit-cascade-failover", "vpnkit-cascade-failover.sh")
			script.WriteString("fi\n")
		}
	}
	return script.String() + "exit 0\n"
}

// AuditConfig checks files on the node and returns only discrepancies and baseline availability.
func AuditConfig(ctx context.Context, cl *remote.Client, names []string, role, iface string) (map[string]ConfigAudit, error) {
	r, err := cl.Run(ctx, configAuditScript(names, role, iface))
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("проверка конфигурации: код выхода %d", r.Code)
	}
	return parseAudit(r.Stdout), nil
}

func parseAudit(out string) map[string]ConfigAudit {
	labels := map[string]string{"changed": "Изменён", "missing": "Отсутствует", "unreadable": "Не удалось прочитать"}
	result := map[string]ConfigAudit{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 2 {
			continue
		}
		a := result[f[0]]
		if f[1] == "baseline" {
			a.Baseline = true
		} else if label, ok := labels[f[1]]; ok && len(f) == 3 {
			issue := label + ": " + f[2]
			if !slices.Contains(a.Issues, issue) {
				a.Issues = append(a.Issues, issue)
			}
		}
		result[f[0]] = a
	}
	return result
}

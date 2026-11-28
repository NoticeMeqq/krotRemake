// Package status — состояние узла одним запросом по SSH (только чтение): система,
// модули установщика, итог проверок vpnkit-watch.
package status

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/modexec"
	"github.com/xDarkOne/krot/internal/remote"
	"github.com/xDarkOne/krot/modules"
)

// Module — модуль установщика на узле (метка /var/lib/vpnkit/modules/<имя>).
type Module struct {
	Name      string   `json:"name"`
	Installed string   `json:"installed"` // когда поставлен (ISO 8601)
	Code      string   `json:"code"`      // версия кода модуля на узле (пусто — поставлен до учёта версий)
	Expected  string   `json:"expected,omitempty"`
	Baseline  bool     `json:"baseline"`
	Issues    []string `json:"issues,omitempty"`
}

// Status — сводка по узлу.
type Status struct {
	Node       string   `json:"node"`
	Role       string   `json:"role"`
	Host       string   `json:"host"`
	Reachable  bool     `json:"reachable"`
	Error      string   `json:"error,omitempty"`
	OS         string   `json:"os"`
	Kernel     string   `json:"kernel"`
	Uptime     string   `json:"uptime"`
	Disk       string   `json:"disk"`
	Mem        string   `json:"mem"`
	Modules    []Module `json:"modules"`
	Watch      bool     `json:"watch"`  // стоит ли мониторинг
	Checks     int      `json:"checks"` // всего проверок vpnkit-watch
	Failed     []string `json:"failed"` // текст непрошедших
	At         string   `json:"at"`
	Compared   bool     `json:"compared"`
	AuditError string   `json:"audit_error,omitempty"`
}

// Проверки — с отдельным файлом состояния: прогон с пульта не влияет на счётчики оповещений
// и сам оповещений не шлёт (для оповещения нужно два провала подряд в одном состоянии).
const script = `export LC_ALL=C.UTF-8
. /etc/os-release 2>/dev/null; echo "OS=$PRETTY_NAME"
echo "KERNEL=$(uname -r)"
echo "UP=$(cut -d. -f1 /proc/uptime)"
echo "DISK=$(df -h / | awk 'NR==2{print $3" из "$2" ("$5")"}')"
echo "MEM=$(awk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} END{printf "%d из %d МБ", (t-a)/1024, t/1024}' /proc/meminfo)"
for f in /var/lib/vpnkit/modules/* /etc/vpnkit/modules/*; do [ -f "$f" ] && echo "MOD=$(basename "$f") $(sed -n 's/^installed=\([^ ]*\).*/\1/p' "$f") $(sed -n 's/.* code=\([0-9a-f]*\).*/\1/p' "$f")"; done
if [ -x /usr/local/sbin/vpnkit-watch.sh ]; then
  echo "WATCH=1"
  WATCH_STATE=/run/vpnkit-status.state timeout 120 /usr/local/sbin/vpnkit-watch.sh 2>&1 | sed 's/^/W=/'
  rm -f /run/vpnkit-status.state
fi
`

// Get собирает состояние узла.
func Get(ctx context.Context, n *inventory.Node) Status {
	return get(ctx, n, false)
}

// Checked also compares installed modules and their files with this build.
func Checked(ctx context.Context, n *inventory.Node) Status {
	return get(ctx, n, true)
}

func get(ctx context.Context, n *inventory.Node, compare bool) Status {
	st := Status{Node: n.Name, Role: n.Role, Host: n.Host, At: time.Now().Format(time.RFC3339)}
	cl, err := remote.Dial(n)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	defer cl.Close()
	st.Reachable = true
	r, err := cl.Run(ctx, script)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(r.Stdout, "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		switch k {
		case "OS":
			st.OS = v
		case "KERNEL":
			st.Kernel = v
		case "UP":
			if sec, err := strconv.Atoi(v); err == nil {
				st.Uptime = Duration(time.Duration(sec) * time.Second)
			}
		case "DISK":
			st.Disk = v
		case "MEM":
			st.Mem = v
		case "MOD":
			f := strings.Fields(v)
			if len(f) == 0 || seen[f[0]] {
				continue
			}
			seen[f[0]] = true
			m := Module{Name: f[0]}
			if len(f) > 1 {
				m.Installed = f[1]
			}
			if len(f) > 2 {
				m.Code = f[2]
			}
			st.Modules = append(st.Modules, m)
		case "WATCH":
			st.Watch = true
		case "W":
			if strings.HasPrefix(v, "✗ ") {
				st.Failed = append(st.Failed, strings.TrimPrefix(v, "✗ "))
			} else if strings.HasPrefix(v, "проверок: ") {
				f := strings.Fields(strings.TrimPrefix(v, "проверок: "))
				if len(f) > 0 {
					st.Checks, _ = strconv.Atoi(strings.TrimSuffix(f[0], ","))
				}
			}
		}
	}
	if compare {
		compareModules(ctx, cl, n, &st)
	}
	return st
}

func compareModules(ctx context.Context, cl *remote.Client, n *inventory.Node, st *Status) {
	st.Compared = true
	current, err := modules.Load()
	if err != nil {
		st.AuditError = err.Error()
		return
	}
	var names []string
	for i := range st.Modules {
		m := &st.Modules[i]
		if _, known := current[m.Name]; known {
			m.Expected = modexec.CodeHash(m.Name)
			names = append(names, m.Name)
		}
	}
	if len(names) == 0 {
		return
	}
	results, err := modexec.AuditConfig(ctx, cl, names, st.Role, modexec.Params(n, "client-awg")["IFACE"])
	if err != nil {
		st.AuditError = err.Error()
		return
	}
	for i := range st.Modules {
		m := &st.Modules[i]
		m.Baseline, m.Issues = results[m.Name].Baseline, results[m.Name].Issues
	}
}

// Duration — «3 дня 4 ч», «5 ч 12 мин», «42 мин».
func Duration(d time.Duration) string {
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return plural(days, "день", "дня", "дней") + " " + strconv.Itoa(h) + " ч"
	case h > 0:
		return strconv.Itoa(h) + " ч " + strconv.Itoa(m) + " мин"
	default:
		return strconv.Itoa(m) + " мин"
	}
}

func plural(n int, one, few, many string) string {
	w := many
	if n%10 == 1 && n%100 != 11 {
		w = one
	} else if n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14) {
		w = few
	}
	return strconv.Itoa(n) + " " + w
}

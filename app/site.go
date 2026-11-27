package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/remote"
)

//go:embed site_remote.py
var siteRemote string

const maxIndexSize = 1024 * 1024

var (
	siteDomain  = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	siteVersion = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type SitePage struct {
	HTML    string `json:"html"`
	Version string `json:"version"`
}

func siteCommand(ctx context.Context, n *inventory.Node, args string, input io.Reader) (string, error) {
	cl, err := remote.Dial(n)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	cmd := "python3 -c \"$(cat <<'KROT_SITE_PY'\n" + siteRemote + "\nKROT_SITE_PY\n)\" " + args
	r, err := cl.RunReader(ctx, cmd, input)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fmt.Errorf("витрина: %s", strings.TrimSpace(r.Stderr))
	}
	return r.Stdout, nil
}

func (a *App) SiteDomains(node string) ([]string, error) {
	n, err := a.node(node)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	out, err := siteCommand(ctx, n, "list", nil)
	if err != nil {
		return nil, err
	}
	var domains []string
	err = json.Unmarshal([]byte(out), &domains)
	return domains, err
}

func (a *App) ReadSite(node, domain string) (SitePage, error) {
	var page SitePage
	if !siteDomain.MatchString(domain) || len(domain) > 253 {
		return page, fmt.Errorf("некорректное доменное имя")
	}
	n, err := a.node(node)
	if err != nil {
		return page, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	out, err := siteCommand(ctx, n, "read "+domain, nil)
	if err != nil {
		return page, err
	}
	err = json.Unmarshal([]byte(out), &page)
	return page, err
}

func (a *App) ChooseSiteDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Папка статического сайта с index.html"})
}

func (a *App) writeSite(node, host, domain, command string, input io.Reader) error {
	if !siteDomain.MatchString(domain) || len(domain) > 253 {
		return fmt.Errorf("некорректное доменное имя")
	}
	a.mu.Lock()
	if a.inv == nil {
		a.mu.Unlock()
		return fmt.Errorf("файл узлов не открыт")
	}
	n, err := a.inv.Node(node)
	if err != nil || n.Host != host {
		a.mu.Unlock()
		return fmt.Errorf("узел изменился. Откройте редактор снова")
	}
	if a.updating || a.busy[node] != "" {
		a.mu.Unlock()
		return fmt.Errorf("дождитесь завершения операции на узле")
	}
	a.busy[node] = "публикация сайта"
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.busy, node); a.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	_, err = siteCommand(ctx, n, command, input)
	return err
}

func (a *App) SaveSiteHTML(node, host, domain, version, html string) error {
	if !siteVersion.MatchString(version) || len(html) == 0 || len(html) > maxIndexSize || !utf8.ValidString(html) {
		return fmt.Errorf("для редактора нужен непустой index.html в UTF-8 размером до 1 МБ")
	}
	return a.writeSite(node, host, domain, "html "+domain+" "+version, strings.NewReader(html))
}

func (a *App) DeploySiteDirectory(node, host, domain, directory string) (int, error) {
	if !filepath.IsAbs(directory) {
		return 0, fmt.Errorf("выберите папку сайта")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	index, err := root.Lstat("index.html")
	if err != nil || !index.Mode().IsRegular() || index.Size() == 0 {
		return 0, fmt.Errorf("в корне папки нужен непустой файл index.html")
	}
	archive, err := os.CreateTemp("", "krot-site-*.tar.gz")
	if err != nil {
		return 0, err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	count, err := archiveSite(root.FS(), archive)
	if err != nil {
		return 0, err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	if err := a.writeSite(node, host, domain, "directory "+domain, archive); err != nil {
		return 0, err
	}
	return count, nil
}

func archiveSite(files fs.FS, out io.Writer) (int, error) {
	pack := gzip.NewWriter(out)
	writer := tar.NewWriter(pack)
	count := 0
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("%s: поддерживаются только обычные файлы и папки", path)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name, header.Mode = path, 0o644
		if info.IsDir() {
			header.Mode = 0o755
		}
		if err := writer.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		file, err := files.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		count++
		_, err = io.Copy(writer, file)
		return err
	})
	tarErr, zipErr := writer.Close(), pack.Close()
	if err != nil {
		return 0, err
	}
	if tarErr != nil {
		return 0, tarErr
	}
	return count, zipErr
}

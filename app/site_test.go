package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestArchiveSitePreservesDependencies(t *testing.T) {
	want := map[string]string{"index.html": "<html></html>", "assets/css/стиль.css": "body {}", "assets/icon.png": "\x89PNG\x00"}
	files := fstest.MapFS{}
	for name, data := range want {
		files[name] = &fstest.MapFile{Data: []byte(data)}
	}
	var buffer bytes.Buffer
	count, err := archiveSite(files, &buffer)
	if err != nil || count != len(want) {
		t.Fatalf("archiveSite = %d, %v", count, err)
	}
	zip, err := gzip.NewReader(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	defer zip.Close()
	reader := tar.NewReader(zip)
	got := map[string]string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			got[header.Name] = string(data)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("archived files = %v, want %v", got, want)
	}
	files["assets/link"] = &fstest.MapFile{Mode: fs.ModeSymlink | 0o777, Data: []byte("../index.html")}
	if _, err := archiveSite(files, io.Discard); err == nil {
		t.Fatal("archiveSite accepted a symlink")
	}
}

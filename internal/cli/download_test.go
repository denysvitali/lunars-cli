package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestRunDownloadSignsAndWritesRedirectedDownload(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/signature":
			if got := r.Header.Get("Cookie"); got != "session=ok" {
				t.Fatalf("Cookie header = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"firmwareVersion": "2024.26.8",
					"firmwareDate": "2024-09-01T00:00:00Z",
					"signature": "sig-a",
					"downloadUrl": "` + server.URL + `/releases/a.ape3"
				}
			]`))
		case "/api/sign-url":
			if got := r.URL.Query().Get("path"); got != "releases/a.ape3" {
				t.Fatalf("sign-url path = %q", got)
			}
			http.Redirect(w, r, server.URL+"/downloads/a.ape3", http.StatusFound)
		case "/downloads/a.ape3":
			w.Header().Set("Content-Disposition", `attachment; filename="firmware.ape3"`)
			_, _ = w.Write([]byte("payload"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	logger := logrus.New()
	logger.SetOutput(ioDiscard{})
	client, err := NewClient(server.URL, "session=ok", logger)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	tmp := t.TempDir()
	var out bytes.Buffer
	err = RunDownload(context.Background(), client, Options{}, "2024.26.8", &out, tmp)
	if err != nil {
		t.Fatalf("RunDownload returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "firmware.ape3"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("downloaded payload = %q", data)
	}
	if !bytes.Contains(out.Bytes(), []byte("Downloaded 7 bytes")) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestRunDownloadRemovesPartialOnFailure(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/signature":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"firmwareVersion": "2024.26.8",
					"signature": "sig-a",
					"downloadUrl": "` + server.URL + `/releases/a.ape3"
				}
			]`))
		case "/api/sign-url":
			http.Redirect(w, r, server.URL+"/downloads/a.ape3", http.StatusFound)
		case "/downloads/a.ape3":
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("bad"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	tmp := t.TempDir()
	var out bytes.Buffer
	err := RunDownload(context.Background(), client, Options{Output: "firmware.ape3"}, "2024.26.8", &out, tmp)
	if err == nil {
		t.Fatal("expected download error")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "firmware.ape3.part")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file was not removed, stat err: %v", statErr)
	}
}

func TestRunDownloadResumeUsesRange(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/signature":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"firmwareVersion": "2024.26.8",
					"signature": "sig-a",
					"downloadUrl": "` + server.URL + `/releases/firmware.ape3"
				}
			]`))
		case "/api/sign-url":
			http.Redirect(w, r, server.URL+"/downloads/firmware.ape3", http.StatusFound)
		case "/downloads/firmware.ape3":
			if got := r.Header.Get("Range"); got != "bytes=3-" {
				t.Fatalf("Range header = %q", got)
			}
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("defg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "firmware.ape3.part"), []byte("abc"), 0644); err != nil {
		t.Fatalf("write partial: %v", err)
	}

	var out bytes.Buffer
	err := RunDownload(context.Background(), client, Options{Output: "firmware.ape3", Resume: true}, "2024.26.8", &out, tmp)
	if err != nil {
		t.Fatalf("RunDownload returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "firmware.ape3"))
	if err != nil {
		t.Fatalf("read completed file: %v", err)
	}
	if string(data) != "abcdefg" {
		t.Fatalf("downloaded payload = %q", data)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "firmware.ape3.part")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file remains, stat err: %v", statErr)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListJSONCommandUsesItsOwnFlag(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/signature" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"firmwareVersion": "2024.26.8",
				"firmwareDate": "2024-09-01T00:00:00Z",
				"signature": "sig-a",
				"downloadUrl": "https://files.lunars.dev/releases/a.ape3"
			}
		]`))
	}))
	defer server.Close()

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"--base-url", server.URL, "--cookie", "session=ok", "list", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr: %s", err, errOut.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "[") {
		t.Fatalf("expected JSON array output, got: %s", out.String())
	}
}

func TestUploadS3UsesXDGConfigBucket(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "lunars")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	config := []byte("s3:\n  bucket: configured-bucket\n  prefix: configured-prefix/\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), config, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"--cookie", "session=ok", "upload", "s3", "firmware/ape3/a.ape3"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr: %s", err, errOut.String())
	}

	got := out.String()
	if !strings.Contains(got, "s3://configured-bucket/configured-prefix/firmware/ape3/a.ape3") {
		t.Fatalf("configured bucket/prefix not used, output: %s", got)
	}
	if !strings.Contains(got, "No signed URL requested") {
		t.Fatalf("expected dry-run output, got: %s", got)
	}
}

func TestUploadS3BucketFlagOverridesXDGConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "lunars")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	config := []byte("s3:\n  bucket: configured-bucket\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), config, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"--cookie", "session=ok", "upload", "s3", "firmware/ape3/a.ape3", "--bucket", "flag-bucket"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr: %s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "s3://flag-bucket/lunars/firmware/ape3/a.ape3") {
		t.Fatalf("bucket flag did not override config, output: %s", out.String())
	}
}

func TestAuthCheckCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/limit" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Cookie"); got != "session=ok" {
			t.Fatalf("Cookie header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"limit":{"allowedLimit":50,"currentCount":9}}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"--base-url", server.URL, "--cookie", "session=ok", "auth", "check"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr: %s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Authenticated. Downloads: 9/50 used, 41 remaining") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestConfigPathUsesXDGConfigHome(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"config", "path"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr: %s", err, errOut.String())
	}

	want := filepath.Join(configHome, "lunars", "config.yaml")
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestConfigInitWritesXDGConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{
		"config", "init",
		"--bucket", "configured-bucket",
		"--prefix", "configured-prefix/",
		"--endpoint-url", "https://s3.example.test",
		"--region", "auto",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr: %s", err, errOut.String())
	}

	configFile := filepath.Join(configHome, "lunars", "config.yaml")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`bucket: "configured-bucket"`,
		`prefix: "configured-prefix/"`,
		`endpoint-url: "https://s3.example.test"`,
		`region: "auto"`,
		`path-style: true`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("config missing %q:\n%s", want, got)
		}
	}

	if !strings.Contains(out.String(), configFile) {
		t.Fatalf("output does not mention config path: %s", out.String())
	}
}

func TestConfigShowAndSet(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"config", "set", "s3.bucket", "set-bucket"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("set bucket returned error: %v\nstderr: %s", err, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	cmd = NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"config", "set", "prefix", "set-prefix/"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("set prefix returned error: %v\nstderr: %s", err, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	cmd = NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"config", "show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show returned error: %v\nstderr: %s", err, errOut.String())
	}

	got := out.String()
	for _, want := range []string{
		`bucket: "set-bucket"`,
		`prefix: "set-prefix/"`,
		`path-style: true`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("config show missing %q:\n%s", want, got)
		}
	}
}

func TestConfigInitRefusesOverwrite(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "lunars")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("s3:\n  bucket: old-bucket\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut, "test")
	cmd.SetArgs([]string{"config", "init", "--bucket", "new-bucket"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected overwrite error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(configFile)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(data), "old-bucket") {
		t.Fatalf("config was overwritten:\n%s", string(data))
	}
}

func TestConfirmS3ExecuteRequiresYes(t *testing.T) {
	var prompt bytes.Buffer

	ok, err := confirmS3Execute(strings.NewReader("no\n"), &prompt)
	if err != nil {
		t.Fatalf("confirmS3Execute returned error: %v", err)
	}
	if ok {
		t.Fatal("expected no confirmation")
	}
	if !strings.Contains(prompt.String(), "consume download quota") {
		t.Fatalf("prompt missing warning: %s", prompt.String())
	}

	ok, err = confirmS3Execute(strings.NewReader("yes\n"), &prompt)
	if err != nil {
		t.Fatalf("confirmS3Execute returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected confirmation")
	}
}

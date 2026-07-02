package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestRunS3UploadDryRunDoesNotSignOrDownload(t *testing.T) {
	t.Parallel()

	signURLCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/signature":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"firmwareVersion": "2024.26.8",
					"firmwareDate": "2024-09-01T00:00:00Z",
					"signature": "sig-a",
					"downloadUrl": "https://files.lunars.dev/releases/a.ape3"
				}
			]`))
		case "/api/sign-url":
			signURLCalled = true
			http.Error(w, "should not sign in dry-run", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	var out bytes.Buffer
	err := RunS3Upload(context.Background(), client, nil, S3UploadOptions{
		Bucket: "bucket",
		Prefix: "mirror/",
	}, "2024.26.8", &out)
	if err != nil {
		t.Fatalf("RunS3Upload returned error: %v", err)
	}
	if signURLCalled {
		t.Fatal("dry-run called /api/sign-url")
	}
	if !bytes.Contains(out.Bytes(), []byte("No signed URL requested")) {
		t.Fatalf("unexpected output: %s", out.String())
	}
	if got, want := S3ObjectKey(S3UploadOptions{Prefix: "mirror/"}, "firmware/ape3/a.ape3"), "mirror/firmware/ape3/a.ape3"; got != want {
		t.Fatalf("S3ObjectKey = %q, want %q", got, want)
	}
}

func TestRunS3UploadExecuteStreamsToUploader(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/signature":
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
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("payload"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	uploader := &recordingUploader{}
	var out bytes.Buffer
	err := RunS3Upload(context.Background(), client, uploader, S3UploadOptions{
		Bucket:  "bucket",
		Prefix:  "mirror/",
		Execute: true,
	}, "2024.26.8", &out)
	if err != nil {
		t.Fatalf("RunS3Upload returned error: %v", err)
	}
	if uploader.upload.Bucket != "bucket" {
		t.Fatalf("bucket = %q", uploader.upload.Bucket)
	}
	if uploader.upload.Key != "mirror/releases/a.ape3" {
		t.Fatalf("key = %q", uploader.upload.Key)
	}
	if string(uploader.body) != "payload" {
		t.Fatalf("body = %q", uploader.body)
	}
	if uploader.upload.ContentType != "application/octet-stream" {
		t.Fatalf("content type = %q", uploader.upload.ContentType)
	}
	if !bytes.Contains(out.Bytes(), []byte("Uploaded releases/a.ape3")) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestAWSS3UploaderUsesPathStyleEndpoint(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	var gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	uploader, err := NewAWSS3Uploader(context.Background(), S3UploadOptions{
		Bucket:      "bucket",
		EndpointURL: server.URL,
		Region:      "us-east-1",
		PathStyle:   true,
	})
	if err != nil {
		t.Fatalf("NewAWSS3Uploader returned error: %v", err)
	}

	err = uploader.PutObject(context.Background(), ObjectUpload{
		Bucket:        "bucket",
		Key:           "mirror/releases/a.ape3",
		Body:          strings.NewReader("payload"),
		ContentLength: int64(len("payload")),
		ContentType:   "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}
	if gotPath != "/bucket/mirror/releases/a.ape3" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody != "payload" {
		t.Fatalf("body = %q", gotBody)
	}
}

func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	client, err := NewClient(baseURL, "session=ok", logger)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	return client
}

type recordingUploader struct {
	upload ObjectUpload
	body   []byte
}

func (u *recordingUploader) PutObject(ctx context.Context, upload ObjectUpload) error {
	u.upload = upload
	body, err := io.ReadAll(upload.Body)
	if err != nil {
		return err
	}
	u.body = body
	return nil
}

package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3UploadOptions struct {
	Bucket      string
	Key         string
	Prefix      string
	EndpointURL string
	Region      string
	PathStyle   bool
	Execute     bool
}

type ObjectUploader interface {
	PutObject(context.Context, ObjectUpload) error
}

type ObjectUpload struct {
	Bucket        string
	Key           string
	Body          io.Reader
	ContentLength int64
	ContentType   string
	Metadata      map[string]string
}

type AWSS3Uploader struct {
	client *s3.Client
}

func NewAWSS3Uploader(ctx context.Context, opts S3UploadOptions) (*AWSS3Uploader, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("--bucket is required when --execute is set")
	}

	var loadOptions []func(*config.LoadOptions) error
	if opts.Region != "" {
		loadOptions = append(loadOptions, config.WithRegion(opts.Region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(s3Options *s3.Options) {
		s3Options.UsePathStyle = opts.PathStyle
		if opts.EndpointURL != "" {
			s3Options.BaseEndpoint = aws.String(opts.EndpointURL)
		}
	})

	return &AWSS3Uploader{client: client}, nil
}

func (u *AWSS3Uploader) PutObject(ctx context.Context, upload ObjectUpload) error {
	input := &s3.PutObjectInput{
		Bucket:   aws.String(upload.Bucket),
		Key:      aws.String(upload.Key),
		Body:     upload.Body,
		Metadata: upload.Metadata,
	}
	if upload.ContentLength >= 0 {
		input.ContentLength = aws.Int64(upload.ContentLength)
	}
	if upload.ContentType != "" {
		input.ContentType = aws.String(upload.ContentType)
	}

	_, err := u.client.PutObject(ctx, input)
	return err
}

func RunS3Upload(ctx context.Context, client *Client, uploader ObjectUploader, opts S3UploadOptions, target string, out io.Writer) error {
	resolved, err := ResolveDownloadTarget(ctx, client, target)
	if err != nil {
		return err
	}

	key := S3ObjectKey(opts, resolved.ArchivePath)
	if opts.Bucket == "" {
		return fmt.Errorf("--bucket is required")
	}

	if !opts.Execute {
		_, err = fmt.Fprintf(out, "DRY RUN: would upload %s to s3://%s/%s\nNo signed URL requested; no download quota used.\n", resolved.ArchivePath, opts.Bucket, key)
		return err
	}
	if uploader == nil {
		return fmt.Errorf("missing S3 uploader")
	}

	signed, err := client.SignPath(ctx, resolved.ArchivePath)
	if err != nil {
		return err
	}

	resp := signed.Response
	if resp == nil {
		resp, err = client.FetchDownload(ctx, signed.URL)
		if err != nil {
			return err
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	contentType := resp.Header.Get("Content-Type")
	length := contentLength(resp)
	metadata := map[string]string{
		"lunars-archive-path": resolved.ArchivePath,
	}
	if resolved.Firmware != nil && resolved.Firmware.FirmwareVersion != "" {
		metadata["lunars-firmware-version"] = resolved.Firmware.FirmwareVersion
	}

	if err := uploader.PutObject(ctx, ObjectUpload{
		Bucket:        opts.Bucket,
		Key:           key,
		Body:          NewProgressReader(resp.Body, length, out, "Uploading"),
		ContentLength: length,
		ContentType:   contentType,
		Metadata:      metadata,
	}); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "Uploaded %s to s3://%s/%s\n", resolved.ArchivePath, opts.Bucket, key)
	return err
}

func S3ObjectKey(opts S3UploadOptions, archivePath string) string {
	if opts.Key != "" {
		return strings.TrimLeft(opts.Key, "/")
	}

	prefix := strings.Trim(opts.Prefix, "/")
	cleanArchivePath := strings.TrimLeft(archivePath, "/")
	if prefix == "" {
		return cleanArchivePath
	}
	return path.Join(prefix, cleanArchivePath)
}

func contentLength(resp *http.Response) int64 {
	if resp.ContentLength >= 0 {
		return resp.ContentLength
	}
	return -1
}

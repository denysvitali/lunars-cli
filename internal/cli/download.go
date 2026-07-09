package cli

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func RunDownload(ctx context.Context, client *Client, opts Options, target string, out io.Writer, cwd string) error {
	resolved, err := ResolveDownloadTargetOpts(ctx, client, target, opts)
	if err != nil {
		return err
	}

	outputPath := ""
	partialPath := ""
	resumeOffset := int64(0)
	if opts.Resume {
		outputPath = ResolveOutputPath(opts.Output, nil, resolved.ArchivePath, cwd)
		partialPath = outputPath + ".part"
		if info, err := os.Stat(partialPath); err == nil {
			resumeOffset = info.Size()
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	signed, err := client.SignPath(ctx, resolved.ArchivePath)
	if err != nil {
		return err
	}

	resp := signed.Response
	if resp == nil {
		resp, err = client.FetchDownloadRange(ctx, signed.URL, resumeOffset)
		if err != nil {
			return err
		}
	} else if resumeOffset > 0 {
		_ = resp.Body.Close()
		return fmt.Errorf("--resume is not supported when lunars.dev streams the signed response directly")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resumeOffset > 0 && resp.StatusCode == http.StatusOK {
		_ = os.Remove(partialPath)
		resumeOffset = 0
	}
	if resumeOffset > 0 && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("download server did not honor resume offset %d", resumeOffset)
	}

	if resumeOffset == 0 {
		outputPath = ResolveOutputPath(opts.Output, resp.Header, resolved.ArchivePath, cwd)
		partialPath = outputPath + ".part"
	}

	if !opts.Force {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", outputPath)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resumeOffset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partialPath, flags, 0600)
	if err != nil {
		return err
	}

	total := contentLength(resp)
	if total >= 0 && resumeOffset > 0 {
		total += resumeOffset
	}
	reader := NewProgressReaderAt(resp.Body, total, resumeOffset, out, "Downloading")
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		if !opts.Resume {
			_ = os.Remove(partialPath)
		}
		return copyErr
	}
	if closeErr != nil {
		if !opts.Resume {
			_ = os.Remove(partialPath)
		}
		return closeErr
	}
	if opts.Force {
		_ = os.Remove(outputPath)
	}
	if err := os.Rename(partialPath, outputPath); err != nil {
		if !opts.Resume {
			_ = os.Remove(partialPath)
		}
		return err
	}

	_, err = fmt.Fprintf(out, "Downloaded %d bytes to %s\n", written+resumeOffset, outputPath)
	return err
}

type ResolvedDownload struct {
	ArchivePath string
	Firmware    *FirmwareRecord
}

func ResolveDownloadTarget(ctx context.Context, client *Client, target string) (ResolvedDownload, error) {
	return ResolveDownloadTargetOpts(ctx, client, target, Options{})
}

func ResolveDownloadTargetOpts(ctx context.Context, client *Client, target string, opts Options) (ResolvedDownload, error) {
	if isHTTPURL(target) || strings.Contains(target, "/") {
		archivePath, err := ArchivePathFromTarget(target)
		return ResolvedDownload{ArchivePath: archivePath}, err
	}

	records, err := client.Signatures(ctx)
	if err != nil {
		return ResolvedDownload{}, err
	}
	match, err := SelectFirmwareOpts(records, target, opts)
	if err == nil {
		archivePath, err := ArchivePathFromTarget(match.DownloadURL)
		if err != nil {
			return ResolvedDownload{}, err
		}
		return ResolvedDownload{ArchivePath: archivePath, Firmware: &match}, nil
	}

	// latest / typed selection should not fall through to archive-path guessing
	if isLatestQuery(target) || opts.Type != "" || opts.PickLatest {
		return ResolvedDownload{}, err
	}

	if strings.Contains(target, ".") {
		archivePath, pathErr := ArchivePathFromTarget(target)
		if pathErr != nil {
			return ResolvedDownload{}, pathErr
		}
		return ResolvedDownload{ArchivePath: archivePath}, nil
	}

	return ResolvedDownload{}, err
}

func ResolveOutputPath(output string, header http.Header, archivePath, cwd string) string {
	fileName := filenameFromDisposition(header.Get("Content-Disposition"))
	if fileName == "" {
		fileName = filepath.Base(strings.TrimRight(archivePath, "/"))
	}
	if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
		fileName = "download"
	}

	if output == "" {
		return filepath.Join(cwd, fileName)
	}

	requested := output
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(cwd, requested)
	}
	if info, err := os.Stat(requested); err == nil && info.IsDir() {
		return filepath.Join(requested, fileName)
	}
	return requested
}

func filenameFromDisposition(disposition string) string {
	if disposition == "" {
		return ""
	}

	_, params, err := mime.ParseMediaType(disposition)
	if err == nil {
		if name := params["filename"]; name != "" {
			return filepath.Base(name)
		}
		if name := params["filename*"]; name != "" {
			return filepath.Base(decodeDispositionFilename(name))
		}
	}

	for _, part := range strings.Split(disposition, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "filename=") {
			return filepath.Base(strings.Trim(strings.TrimPrefix(part, "filename="), `"`))
		}
	}
	return ""
}

func decodeDispositionFilename(value string) string {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "utf-8''") {
		if decoded, err := url.PathUnescape(value[len("utf-8''"):]); err == nil {
			return decoded
		}
	}
	return value
}

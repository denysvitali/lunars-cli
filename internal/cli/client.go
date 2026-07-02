package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const userAgent = "lunars-cli/0.1.0"

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	baseURL          *url.URL
	cookie           string
	httpClient       httpDoer
	noRedirectClient httpDoer
	logger           *logrus.Logger
}

type SignedDownload struct {
	URL      string
	Response *http.Response
}

func NewClient(baseURL, cookie string, logger *logrus.Logger) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q", baseURL)
	}
	if logger == nil {
		logger = logrus.New()
		logger.SetOutput(io.Discard)
	}

	return &Client{
		baseURL: parsed,
		cookie:  cookie,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
		noRedirectClient: &http.Client{
			Timeout: 10 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger: logger,
	}, nil
}

func (c *Client) Signatures(ctx context.Context) ([]FirmwareRecord, error) {
	var records []FirmwareRecord
	if err := c.getJSON(ctx, "/api/signature", &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (c *Client) Limit(ctx context.Context) (LimitResponse, error) {
	var limit LimitResponse
	if err := c.getJSON(ctx, "/api/limit", &limit); err != nil {
		return LimitResponse{}, err
	}
	return limit, nil
}

func (c *Client) SignPath(ctx context.Context, archivePath string) (SignedDownload, error) {
	endpoint := c.endpoint("/api/sign-url")
	query := endpoint.Query()
	query.Set("path", archivePath)
	endpoint.RawQuery = query.Encode()

	c.logger.WithField("path", archivePath).Debug("signing download URL")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return SignedDownload{}, err
	}
	c.addAuthHeaders(req, "*/*")

	resp, err := c.noRedirectClient.Do(req)
	if err != nil {
		return SignedDownload{}, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return SignedDownload{}, errUnauthorized()
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_ = resp.Body.Close()
		location := resp.Header.Get("Location")
		if location == "" {
			return SignedDownload{}, fmt.Errorf("sign-url redirected without a Location header")
		}
		downloadURL, err := endpoint.Parse(location)
		if err != nil {
			return SignedDownload{}, err
		}
		return SignedDownload{URL: downloadURL.String()}, nil
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		defer func() {
			_ = resp.Body.Close()
		}()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return SignedDownload{}, err
		}
		if err := responseError(resp, body, "/api/sign-url"); err != nil {
			return SignedDownload{}, err
		}

		var payload struct {
			URL         string `json:"url"`
			DownloadURL string `json:"downloadUrl"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return SignedDownload{}, fmt.Errorf("expected JSON from /api/sign-url: %w", err)
		}
		if payload.URL != "" {
			return SignedDownload{URL: payload.URL}, nil
		}
		if payload.DownloadURL != "" {
			return SignedDownload{URL: payload.DownloadURL}, nil
		}
		return SignedDownload{}, fmt.Errorf("sign-url returned JSON without a download URL")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return SignedDownload{}, fmt.Errorf("signing failed with HTTP %d", resp.StatusCode)
	}

	return SignedDownload{Response: resp}, nil
}

func (c *Client) FetchDownload(ctx context.Context, downloadURL string) (*http.Response, error) {
	return c.FetchDownloadRange(ctx, downloadURL, 0)
}

func (c *Client) FetchDownloadRange(ctx context.Context, downloadURL string, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download server rejected resume offset %d", offset)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	endpoint := c.endpoint(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	c.addAuthHeaders(req, "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := responseError(resp, body, path); err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("expected JSON from %s: %w", path, err)
	}
	return nil
}

func (c *Client) endpoint(path string) *url.URL {
	ref := &url.URL{Path: path}
	return c.baseURL.ResolveReference(ref)
}

func (c *Client) addAuthHeaders(req *http.Request, accept string) {
	req.Header.Set("Accept", accept)
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("User-Agent", userAgent)
}

func responseError(resp *http.Response, body []byte, path string) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized()
	}

	var payload struct {
		Error string `json:"error"`
	}
	if len(bytes.TrimSpace(body)) > 0 && json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		return fmt.Errorf("%s", payload.Error)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request failed with HTTP %d: %s", resp.StatusCode, path)
	}
	return nil
}

func errUnauthorized() error {
	return fmt.Errorf("unauthorized; refresh your lunars.dev session cookie and confirm sponsor access")
}

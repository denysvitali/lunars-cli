package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type FirmwareRecord struct {
	FirmwareVersion string
	FirmwareDate    string
	Signature       string
	DownloadURL     string
	Raw             map[string]any
}

func (f *FirmwareRecord) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Raw = raw
	f.FirmwareVersion = stringField(raw, "firmwareVersion")
	f.FirmwareDate = stringField(raw, "firmwareDate")
	f.Signature = stringField(raw, "signature")
	f.DownloadURL = stringField(raw, "downloadUrl")
	return nil
}

func (f FirmwareRecord) MarshalJSON() ([]byte, error) {
	if f.Raw != nil {
		return json.Marshal(f.Raw)
	}
	return json.Marshal(map[string]any{
		"firmwareVersion": f.FirmwareVersion,
		"firmwareDate":    f.FirmwareDate,
		"signature":       f.Signature,
		"downloadUrl":     f.DownloadURL,
	})
}

type LimitResponse struct {
	Limit *DownloadLimit `json:"limit"`
}

type DownloadLimit struct {
	AllowedLimit int `json:"allowedLimit"`
	CurrentCount int `json:"currentCount"`
}

type FirmwareMatchError struct {
	Query   string
	Matches []FirmwareRecord
}

func (e FirmwareMatchError) Error() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "multiple firmware records matched %q; choose one of:\n", e.Query)
	for _, record := range e.Matches[:min(8, len(e.Matches))] {
		fmt.Fprintf(&builder, "  %s\n", firmwareChoice(record))
	}
	if len(e.Matches) > 8 {
		fmt.Fprintf(&builder, "  ...and %d more\n", len(e.Matches)-8)
	}
	builder.WriteString("Use an exact signature, archive path, files.lunars.dev URL, --type, or --pick-latest.")
	return builder.String()
}

func SelectFirmware(records []FirmwareRecord, query string) (FirmwareRecord, error) {
	return SelectFirmwareOpts(records, query, Options{})
}

func SelectFirmwareOpts(records []FirmwareRecord, query string, opts Options) (FirmwareRecord, error) {
	records = FilterFirmware(records, Options{Type: opts.Type})
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return FirmwareRecord{}, fmt.Errorf("firmware query is empty")
	}

	if isLatestQuery(normalized) {
		return SelectLatestFirmware(records)
	}

	var exact []FirmwareRecord
	for _, record := range records {
		for _, value := range []string{record.Signature, record.FirmwareVersion, baseNameFromURL(record.DownloadURL)} {
			if value != "" && strings.ToLower(value) == normalized {
				exact = append(exact, record)
				break
			}
		}
	}

	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		if opts.PickLatest {
			return pickLatestAmong(exact, query)
		}
		return FirmwareRecord{}, FirmwareMatchError{Query: query, Matches: exact}
	}

	var partial []FirmwareRecord
	for _, record := range records {
		for _, value := range []string{record.Signature, record.FirmwareVersion, record.DownloadURL, record.searchBlob()} {
			if value != "" && strings.Contains(strings.ToLower(value), normalized) {
				partial = append(partial, record)
				break
			}
		}
	}

	if len(partial) == 1 {
		return partial[0], nil
	}
	if len(partial) > 1 {
		if opts.PickLatest {
			return pickLatestAmong(partial, query)
		}
		return FirmwareRecord{}, FirmwareMatchError{Query: query, Matches: partial}
	}

	return FirmwareRecord{}, fmt.Errorf("no firmware record matched %q; use \"lunars list --search %s\" to inspect matches", query, query)
}

func SelectLatestFirmware(records []FirmwareRecord) (FirmwareRecord, error) {
	if len(records) == 0 {
		return FirmwareRecord{}, fmt.Errorf("no firmware records available")
	}
	return pickLatestAmong(records, "latest")
}

func pickLatestAmong(records []FirmwareRecord, query string) (FirmwareRecord, error) {
	if len(records) == 0 {
		return FirmwareRecord{}, fmt.Errorf("no firmware records matched %q", query)
	}
	if len(records) == 1 {
		return records[0], nil
	}

	sorted := append([]FirmwareRecord(nil), records...)
	SortFirmwareNewestFirst(sorted)
	newestVersion := sorted[0].FirmwareVersion
	sameVersion := make([]FirmwareRecord, 0, len(sorted))
	for _, record := range sorted {
		if record.FirmwareVersion == newestVersion {
			sameVersion = append(sameVersion, record)
		}
	}
	if len(sameVersion) == 1 {
		return sameVersion[0], nil
	}
	return FirmwareRecord{}, FirmwareMatchError{Query: query, Matches: sameVersion}
}

func isLatestQuery(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "latest", "newest":
		return true
	default:
		return false
	}
}

// CompareFirmwareVersion compares Tesla-style versions (year.week.patch...).
// Higher/newer versions compare greater. Empty versions sort lowest.
func CompareFirmwareVersion(a, b string) int {
	as := versionParts(a)
	bs := versionParts(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	// Fall back to raw string when numeric parts equal (e.g. suffix noise).
	return strings.Compare(a, b)
}

func versionParts(version string) []int {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	// Strip common non-numeric prefixes/suffixes by splitting on non-digits.
	parts := make([]int, 0, 4)
	current := 0
	inNumber := false
	for _, r := range version {
		if r >= '0' && r <= '9' {
			current = current*10 + int(r-'0')
			inNumber = true
			continue
		}
		if inNumber {
			parts = append(parts, current)
			current = 0
			inNumber = false
		}
	}
	if inNumber {
		parts = append(parts, current)
	}
	return parts
}

func SortFirmwareNewestFirst(records []FirmwareRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if cmp := CompareFirmwareVersion(records[i].FirmwareVersion, records[j].FirmwareVersion); cmp != 0 {
			return cmp > 0
		}
		// Prefer newer firmwareDate when versions tie.
		return records[i].FirmwareDate > records[j].FirmwareDate
	})
}

func firmwareChoice(record FirmwareRecord) string {
	archivePath := ""
	if record.DownloadURL != "" {
		archivePath, _ = ArchivePathFromTarget(record.DownloadURL)
	}
	parts := make([]string, 0, 4)
	if record.FirmwareVersion != "" {
		parts = append(parts, "version="+record.FirmwareVersion)
	}
	if record.Signature != "" {
		parts = append(parts, "signature="+record.Signature)
	}
	if archivePath != "" {
		parts = append(parts, "path="+archivePath)
	}
	return strings.Join(parts, "  ")
}

func FilterFirmware(records []FirmwareRecord, opts Options) []FirmwareRecord {
	search := strings.ToLower(opts.Search)
	fileType := strings.ToLower(opts.Type)

	filtered := make([]FirmwareRecord, 0, len(records))
	for _, record := range records {
		if fileType != "" && fileTypeFor(record.DownloadURL) != fileType {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(record.searchBlob()), search) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

func ArchivePathFromTarget(target string) (string, error) {
	pathname := target
	if isHTTPURL(target) {
		parsed, err := url.Parse(target)
		if err != nil {
			return "", err
		}
		pathname = parsed.Path
	}

	archivePath := strings.Trim(pathname, "/")
	if archivePath == "" {
		return "", fmt.Errorf("download target resolved to an empty archive path")
	}
	return archivePath, nil
}

func RenderFirmwareTable(records []FirmwareRecord) string {
	if len(records) == 0 {
		return "No firmware records matched.\n"
	}

	SortFirmwareNewestFirst(records)

	rows := make([]map[string]string, 0, len(records))
	for _, record := range records {
		archivePath := ""
		if record.DownloadURL != "" {
			archivePath, _ = ArchivePathFromTarget(record.DownloadURL)
		}
		rows = append(rows, map[string]string{
			"VERSION":   record.FirmwareVersion,
			"DATE":      formatFirmwareDate(record.FirmwareDate),
			"TYPE":      fileTypeFor(record.DownloadURL),
			"SIGNATURE": record.Signature,
			"PATH":      archivePath,
		})
	}

	headers := []string{"VERSION", "DATE", "TYPE", "SIGNATURE", "PATH"}
	widths := make(map[string]int, len(headers))
	for _, header := range headers {
		widths[header] = len(header)
		for _, row := range rows {
			if len(row[header]) > widths[header] {
				widths[header] = len(row[header])
			}
		}
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	var builder strings.Builder
	for i, header := range headers {
		if i > 0 {
			builder.WriteString("  ")
		}
		builder.WriteString(headerStyle.Render(padRight(header, widths[header])))
	}
	builder.WriteByte('\n')

	for _, row := range rows {
		for i, header := range headers {
			if i > 0 {
				builder.WriteString("  ")
			}
			builder.WriteString(padRight(row[header], widths[header]))
		}
		builder.WriteByte('\n')
	}

	return builder.String()
}

func stringField(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func (f FirmwareRecord) searchBlob() string {
	if f.Raw == nil {
		data, _ := json.Marshal(f)
		return string(data)
	}
	data, _ := json.Marshal(f.Raw)
	return string(data)
}

func fileTypeFor(downloadURL string) string {
	fileName := baseNameFromURL(downloadURL)
	parts := strings.Split(fileName, ".")
	if len(parts) < 2 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-1])
}

func baseNameFromURL(value string) string {
	if value == "" {
		return ""
	}
	if isHTTPURL(value) {
		if parsed, err := url.Parse(value); err == nil {
			return path.Base(strings.TrimRight(parsed.Path, "/"))
		}
	}
	return path.Base(strings.TrimRight(value, "/"))
}

func formatFirmwareDate(value string) string {
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format("2006-01-02")
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("2006-01-02")
	}
	return value
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://")
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

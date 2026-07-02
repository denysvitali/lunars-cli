package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func testFirmware(t *testing.T) []FirmwareRecord {
	t.Helper()

	data := []byte(`[
		{
			"firmwareVersion": "2024.26.8",
			"firmwareDate": "2024-09-01T00:00:00Z",
			"signature": "sig-a",
			"downloadUrl": "https://files.lunars.dev/releases/a.ape3"
		},
		{
			"firmwareVersion": "2024.32.1",
			"firmwareDate": "2024-10-01T00:00:00Z",
			"signature": "sig-b",
			"downloadUrl": "https://files.lunars.dev/releases/b.mcu2"
		}
	]`)

	var records []FirmwareRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("unmarshal test firmware: %v", err)
	}
	return records
}

func TestArchivePathFromTarget(t *testing.T) {
	t.Parallel()

	got, err := ArchivePathFromTarget("https://files.lunars.dev/releases/firmware.bin?ignored=true")
	if err != nil {
		t.Fatalf("ArchivePathFromTarget returned error: %v", err)
	}
	if got != "releases/firmware.bin" {
		t.Fatalf("got %q", got)
	}
}

func TestSelectFirmware(t *testing.T) {
	t.Parallel()
	records := testFirmware(t)

	match, err := SelectFirmware(records, "2024.26.8")
	if err != nil {
		t.Fatalf("SelectFirmware returned error: %v", err)
	}
	if match.Signature != "sig-a" {
		t.Fatalf("got signature %q", match.Signature)
	}

	if _, err := SelectFirmware(records, "2024"); err == nil {
		t.Fatal("expected ambiguous partial match error")
	} else if !strings.Contains(err.Error(), "signature=sig-a") || !strings.Contains(err.Error(), "path=releases/b.mcu2") {
		t.Fatalf("ambiguous error did not include choices: %v", err)
	}
}

func TestFilterFirmware(t *testing.T) {
	t.Parallel()
	records := testFirmware(t)

	filtered := FilterFirmware(records, Options{Search: "2024.32", Type: "mcu2"})
	if len(filtered) != 1 || filtered[0].Signature != "sig-b" {
		t.Fatalf("unexpected filtered records: %+v", filtered)
	}
}

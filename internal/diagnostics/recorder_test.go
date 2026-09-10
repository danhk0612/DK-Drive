package diagnostics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentCountsPrivacyAndIsolation(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir, "https://secret-user:secret-password@host/private")
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(dir, "sftp")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := Call(r, LogicalStat, func() (int, error) { return 7, errors.New("secret-error-path") })
			if v != 7 || e == nil {
				t.Error("changed result")
			}
		}()
	}
	wg.Wait()
	before := r.Snapshot()
	if before.Metrics[LogicalStat].Calls != 100 || before.Metrics[LogicalStat].Errors != 100 || before.Metrics[LogicalStat].TotalNS <= 0 || before.Metrics[LogicalStat].MaxNS <= 0 {
		t.Fatalf("%+v", before)
	}
	if other.Snapshot().Metrics[LogicalStat].Calls != 0 {
		t.Fatal("shared counters")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(r.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
		t.Fatalf("private data leaked: %s", data)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Protocol != "unknown" || report.Metrics[LogicalStat].Calls != 100 {
		t.Fatalf("%+v", report)
	}
	Call(r, LogicalStat, func() (int, error) { return 1, nil })
	if r.Snapshot().Metrics[LogicalStat].Calls != 100 {
		t.Fatal("changed closed report")
	}
}
func TestDisabledAndInvalidOutput(t *testing.T) {
	r, err := Open("", "sftp")
	if err != nil || r != nil {
		t.Fatal("disabled")
	}
	value, err := Call(r, LogicalStat, func() (int, error) { return 4, nil })
	if value != 4 || err != nil {
		t.Fatal("disabled passthrough")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(file, "sftp"); err == nil {
		t.Fatal("accepted file as directory")
	}
}

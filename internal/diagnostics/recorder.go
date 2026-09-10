// Package diagnostics records bounded metadata counters without resource names or error text.
package diagnostics

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Operation uint8

const (
	LogicalStat Operation = iota
	LogicalReadDir
	WebDAVDepth0
	WebDAVDepth1
	FTPGetEntry
	FTPList
	SFTPStat
	SFTPReadDir
	operationCount
)

var names = [operationCount]string{"logical.stat", "logical.readdir", "webdav.propfind.depth0", "webdav.propfind.depth1", "ftp.getentry", "ftp.list", "sftp.stat", "sftp.readdir"}

type Metric struct {
	Operation string `json:"operation"`
	Calls     uint64 `json:"calls"`
	Errors    uint64 `json:"errors"`
	TotalNS   int64  `json:"total_ns"`
	MaxNS     int64  `json:"max_ns"`
}
type Report struct {
	Schema    int                    `json:"schema"`
	Protocol  string                 `json:"protocol"`
	Started   time.Time              `json:"started_utc"`
	ElapsedNS int64                  `json:"elapsed_ns"`
	Metrics   [operationCount]Metric `json:"metrics"`
}
type Recorder struct {
	mu       sync.Mutex
	report   Report
	started  time.Time
	file     *os.File
	closed   bool
	closeErr error
}

// Open creates one report per connection only when the caller supplies a directory.
// Protocol is restricted to known labels; profile names and URLs cannot be recorded.
func Open(directory, protocol string) (*Recorder, error) {
	if directory == "" {
		return nil, nil
	}
	switch protocol {
	case "sftp", "webdav", "ftp", "explicit-ftps", "implicit-ftps":
	default:
		protocol = "unknown"
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(directory, "dkdrive-metadata-*.json")
	if err != nil {
		return nil, err
	}
	r := &Recorder{file: file, started: time.Now()}
	r.report = Report{Schema: 1, Protocol: protocol, Started: r.started.UTC()}
	for op := Operation(0); op < operationCount; op++ {
		r.report.Metrics[op].Operation = names[op]
	}
	return r, nil
}

// Call counts each executed attempt, including retries. A nil recorder is a direct call.
func Call[T any](r *Recorder, op Operation, call func() (T, error)) (T, error) {
	if r == nil {
		return call()
	}
	started := time.Now()
	result, err := call()
	elapsed := time.Since(started).Nanoseconds()
	r.mu.Lock()
	if !r.closed && op < operationCount {
		m := &r.report.Metrics[op]
		m.Calls++
		if err != nil {
			m.Errors++
		}
		m.TotalNS += elapsed
		if elapsed > m.MaxNS {
			m.MaxNS = elapsed
		}
	}
	r.mu.Unlock()
	return result, err
}

func (r *Recorder) Snapshot() Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	report := r.report
	report.ElapsedNS = time.Since(r.started).Nanoseconds()
	return report
}

// Close writes once, after the connection closes. Forced process termination may leave an empty file.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	r.report.ElapsedNS = time.Since(r.started).Nanoseconds()
	encodeErr := json.NewEncoder(r.file).Encode(r.report)
	closeErr := r.file.Close()
	if encodeErr != nil {
		r.closeErr = encodeErr
	} else {
		r.closeErr = closeErr
	}
	return r.closeErr
}

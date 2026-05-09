package s07

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Recorder appends RolloutItems to one JSONL file, one item per line.
//
// Concurrency: protected by an internal mutex; safe to call from multiple
// goroutines. Writes are O_APPEND so even a crash mid-write only loses the
// in-flight line, not earlier ones.
type Recorder struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

// New opens / creates `path` for append. Parent dirs are created.
// `meta` is recorded as the first line of a new file.
//
// If the file already exists and has content, `meta` is NOT written — the
// caller is resuming an existing session and the prior meta line stands.
func New(path string, meta SessionMeta) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	st, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	r := &Recorder{path: path, f: f}
	if os.IsNotExist(statErr) || (st != nil && st.Size() == 0) {
		if err := r.writeMeta(meta); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return r, nil
}

func (r *Recorder) writeMeta(meta SessionMeta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return r.appendItem(KindSessionMeta, b)
}

// Record persists one item. `payload` is anything json-marshalable.
func (r *Recorder) Record(kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return r.appendItem(kind, b)
}

func (r *Recorder) appendItem(kind string, payload json.RawMessage) error {
	item := RolloutItem{Kind: kind, TS: time.Now().UnixMilli(), Payload: payload}
	line, err := json.Marshal(item)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// Close releases the file handle. The file is left on disk for resume.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// Path returns the on-disk path.
func (r *Recorder) Path() string { return r.path }

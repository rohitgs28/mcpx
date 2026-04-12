package config

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const minimalYAML = `
listen: ":8080"
servers:
  - name: filesystem
    url: http://localhost:3001
`

const updatedYAML = `
listen: ":9090"
servers:
  - name: filesystem
    url: http://localhost:3001
  - name: database
    url: http://localhost:3002
`

const invalidYAML = `
listen: ""
servers:
  - name: filesystem
    url: http://localhost:3001
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestWatch_ReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcpx.yaml")
	writeFile(t, path, minimalYAML)

	var reloads atomic.Int32
	gotCfg := make(chan *Config, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Watch(ctx, path, 20*time.Millisecond, func(c *Config) error {
		reloads.Add(1)
		gotCfg <- c
		return nil
	}, func(err error) {
		t.Errorf("unexpected watcher error: %v", err)
	})

	// Wait a tick — the initial state should NOT trigger a reload.
	time.Sleep(80 * time.Millisecond)
	if got := reloads.Load(); got != 0 {
		t.Fatalf("expected no reload before file change, got %d", got)
	}

	// Ensure the mtime actually advances on filesystems with coarse
	// timestamp resolution (some Windows FS have ~100ms granularity).
	time.Sleep(150 * time.Millisecond)
	writeFile(t, path, updatedYAML)

	select {
	case c := <-gotCfg:
		if c.Listen != ":9090" {
			t.Errorf("expected listen :9090, got %q", c.Listen)
		}
		if len(c.Servers) != 2 {
			t.Errorf("expected 2 servers after reload, got %d", len(c.Servers))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not reload within 2s")
	}
}

func TestWatch_InvalidConfigReportedButKeepsWatching(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcpx.yaml")
	writeFile(t, path, minimalYAML)

	var reloads atomic.Int32
	var errs atomic.Int32
	gotCfg := make(chan *Config, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Watch(ctx, path, 20*time.Millisecond, func(c *Config) error {
		reloads.Add(1)
		gotCfg <- c
		return nil
	}, func(err error) {
		errs.Add(1)
	})

	// First write an invalid config.
	time.Sleep(150 * time.Millisecond)
	writeFile(t, path, invalidYAML)

	// Wait for the error to be reported.
	deadline := time.Now().Add(2 * time.Second)
	for errs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if errs.Load() == 0 {
		t.Fatal("expected an error for invalid config")
	}
	if reloads.Load() != 0 {
		t.Fatalf("expected no successful reload for invalid config, got %d", reloads.Load())
	}

	// Recovery: write a valid updated config, watcher should reload.
	time.Sleep(150 * time.Millisecond)
	writeFile(t, path, updatedYAML)

	select {
	case c := <-gotCfg:
		if c.Listen != ":9090" {
			t.Errorf("expected listen :9090 after recovery, got %q", c.Listen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not recover after invalid config")
	}
}

func TestWatch_CancelStops(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcpx.yaml")
	writeFile(t, path, minimalYAML)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Watch(ctx, path, 20*time.Millisecond, func(*Config) error { return nil }, nil)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Watch did not return after context cancel")
	}
}

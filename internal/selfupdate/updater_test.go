package selfupdate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/relayx-tech/agent/internal/selfupdate"
)

type fakeDownloader struct {
	data []byte
	err  error
	gate chan struct{}
}

func (f *fakeDownloader) Get(context.Context, string) ([]byte, error) {
	if f.gate != nil {
		<-f.gate
	}
	return f.data, f.err
}

type fakeRestarter struct{ fail bool }

func (f *fakeRestarter) Restart(context.Context, string) error {
	if f.fail {
		return errors.New("systemctl failed")
	}
	return nil
}

type fakeLinker struct {
	current  string
	switches []string
}

func (f *fakeLinker) Current(string) (string, error) { return f.current, nil }
func (f *fakeLinker) Switch(_ string, target string) error {
	f.switches = append(f.switches, target)
	f.current = target
	return nil
}

type memStore struct {
	mu    sync.Mutex
	state selfupdate.State
}

func (m *memStore) Load() (selfupdate.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, nil
}
func (m *memStore) Save(s selfupdate.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	return nil
}

func waitTerminal(t *testing.T, u *selfupdate.Updater) selfupdate.State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := u.Status()
		switch state.State {
		case "succeeded", "failed", "rolled_back":
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("update did not finish: %+v", u.Status())
	return selfupdate.State{}
}

func TestUpdateSuccess(t *testing.T) {
	payload := []byte("agent-binary")
	sum := sha256.Sum256(payload)
	store := &memStore{}
	linker := &fakeLinker{}
	u := selfupdate.New(
		selfupdate.Config{BaseDir: t.TempDir(), ServiceName: "relayx-agent.service", Timeout: time.Second},
		&fakeDownloader{data: payload}, &fakeRestarter{}, linker, store,
		func() string { return "1.3.0" },
	)
	result := u.Update(context.Background(), selfupdate.Request{
		OperationID: "op-1", TargetVersion: "1.4.0", SourceURL: "https://example.com/agent",
		SHA256: hex.EncodeToString(sum[:]),
	})
	if !result.Accepted {
		t.Fatalf("update rejected: %+v", result)
	}
	state := waitTerminal(t, u)
	if state.State != "succeeded" || state.TargetVersion != "1.4.0" {
		t.Fatalf("state = %+v", state)
	}
	if len(linker.switches) != 1 {
		t.Fatalf("switches = %+v", linker.switches)
	}
}

func TestUpdateIdempotentAndConflict(t *testing.T) {
	payload := []byte("agent-binary")
	sum := sha256.Sum256(payload)
	gate := make(chan struct{})
	u := selfupdate.New(
		selfupdate.Config{BaseDir: t.TempDir(), ServiceName: "relayx-agent.service", Timeout: time.Second},
		&fakeDownloader{data: payload, gate: gate}, &fakeRestarter{}, &fakeLinker{}, &memStore{},
		func() string { return "1.3.0" },
	)
	req := selfupdate.Request{OperationID: "op-1", TargetVersion: "1.4.0", SourceURL: "https://example.com/agent", SHA256: hex.EncodeToString(sum[:])}
	if !u.Update(context.Background(), req).Accepted {
		t.Fatal("first update rejected")
	}
	// 同 op 幂等。
	if got := u.Update(context.Background(), req); !got.Accepted {
		t.Fatalf("same op should be idempotent: %+v", got)
	}
	// 不同 op 且进行中 → UPDATE_IN_PROGRESS。
	other := req
	other.OperationID = "op-2"
	got := u.Update(context.Background(), other)
	if got.Accepted || got.ErrorCode != "UPDATE_IN_PROGRESS" {
		t.Fatalf("conflict result = %+v", got)
	}
	close(gate)
	_ = waitTerminal(t, u)
}

func TestUpdateRollbackOnRestartFailure(t *testing.T) {
	payload := []byte("agent-binary")
	sum := sha256.Sum256(payload)
	linker := &fakeLinker{current: "releases/1.3.0"}
	u := selfupdate.New(
		selfupdate.Config{BaseDir: t.TempDir(), ServiceName: "relayx-agent.service", Timeout: time.Second},
		&fakeDownloader{data: payload}, &fakeRestarter{fail: true}, linker, &memStore{},
		func() string { return "1.3.0" },
	)
	u.Update(context.Background(), selfupdate.Request{
		OperationID: "op-1", TargetVersion: "1.4.0", SourceURL: "https://example.com/agent", SHA256: hex.EncodeToString(sum[:]),
	})
	state := waitTerminal(t, u)
	if state.State != "rolled_back" {
		t.Fatalf("state = %+v, want rolled_back", state)
	}
	if linker.current != "releases/1.3.0" {
		t.Fatalf("link not rolled back: %s", linker.current)
	}
}

// Package selfupdate 实现 Agent 在线自升级（design.md §7.1）。
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config 是 Agent 自升级配置。
type Config struct {
	BaseDir     string
	CurrentLink string
	ServiceName string
	PublicKeys  []string // hex ed25519
	Timeout     time.Duration
}

// State 是升级状态（与合同 AgentUpdateState 对齐）。
type State struct {
	OperationID   string    `json:"operationId"`
	State         string    `json:"state"`
	TargetVersion string    `json:"targetVersion"`
	Progress      int       `json:"progress"`
	ErrorCode     string    `json:"errorCode"`
	ErrorMessage  string    `json:"errorMessage"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Request 是 Update 输入。
type Request struct {
	OperationID   string
	TargetVersion string
	SourceURL     string
	SHA256        string
	Signature     string
	Reason        string
	Deadline      time.Duration
}

// Result 是 Update 返回值。
type Result struct {
	Accepted       bool
	State          string
	CurrentVersion string
	ErrorCode      string
	ErrorMessage   string
}

// Downloader 拉取升级资产。
type Downloader interface {
	Get(ctx context.Context, url string) ([]byte, error)
}

// Restarter 重启 relayx-agent 服务。
type Restarter interface {
	Restart(ctx context.Context, service string) error
}

// Linker 切换 current 软链。
type Linker interface {
	Current(baseDir string) (string, error)
	Switch(baseDir, target string) error
}

// Store 持久化状态。
type Store interface {
	Load() (State, error)
	Save(State) error
}

// Updater 是自升级执行器。
type Updater struct {
	cfg            Config
	download       Downloader
	restart        Restarter
	linker         Linker
	store          Store
	currentVersion func() string
	healthCheck    func(ctx context.Context) error

	mu    sync.Mutex
	state State
}

func New(cfg Config, download Downloader, restart Restarter, linker Linker, store Store, currentVersion func() string) *Updater {
	u := &Updater{
		cfg: cfg, download: download, restart: restart, linker: linker, store: store,
		currentVersion: currentVersion,
	}
	if store != nil {
		if state, err := store.Load(); err == nil {
			u.state = state
		}
	}
	return u
}

// SetHealthCheck 注入健康检查（默认 2s 轮询 core-node socket）。
func (u *Updater) SetHealthCheck(fn func(ctx context.Context) error) { u.healthCheck = fn }

// Status 返回当前升级状态。
func (u *Updater) Status() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// Update 发起升级：同 operationId 幂等；进行中收到不同 op 返回 UPDATE_IN_PROGRESS。
func (u *Updater) Update(ctx context.Context, req Request) Result {
	u.mu.Lock()
	if u.state.OperationID == req.OperationID && req.OperationID != "" {
		state := u.state
		u.mu.Unlock()
		return Result{Accepted: true, State: state.State, CurrentVersion: u.currentVersion(), ErrorCode: state.ErrorCode, ErrorMessage: state.ErrorMessage}
	}
	if isRunning(u.state.State) {
		u.mu.Unlock()
		return Result{Accepted: false, State: u.state.State, CurrentVersion: u.currentVersion(), ErrorCode: "UPDATE_IN_PROGRESS", ErrorMessage: "another update is in progress"}
	}
	u.state = State{
		OperationID: req.OperationID, State: "pending", TargetVersion: req.TargetVersion,
		Progress: 0, UpdatedAt: time.Now().UTC(),
	}
	u.persistLocked()
	u.mu.Unlock()

	go u.run(req)
	return Result{Accepted: true, State: "pending", CurrentVersion: u.currentVersion()}
}

func (u *Updater) run(req Request) {
	timeout := req.Deadline
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	previous, _ := u.linker.Current(u.cfg.BaseDir)

	data, err := u.download.Get(ctx, req.SourceURL)
	if err != nil {
		u.fail(req, previous, "UPSTREAM_UPDATE_SOURCE_FAILED", err.Error())
		return
	}
	u.set(req, "verifying", 40)
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if req.SHA256 != "" && !strings.EqualFold(got, req.SHA256) {
		u.fail(req, previous, "UPSTREAM_SIGNATURE_INVALID", "sha256 mismatch")
		return
	}
	if len(u.cfg.PublicKeys) > 0 {
		if err := verifySignature(sum[:], req.Signature, u.cfg.PublicKeys); err != nil {
			u.fail(req, previous, "UPSTREAM_SIGNATURE_INVALID", err.Error())
			return
		}
	}

	u.set(req, "applying", 70)
	releaseDir := filepath.Join(u.cfg.BaseDir, "releases", req.TargetVersion)
	if err := os.MkdirAll(releaseDir, 0o750); err != nil {
		u.fail(req, previous, "CONFIG_INVALID", err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "relayx-agent"), data, 0o750); err != nil {
		u.fail(req, previous, "CONFIG_INVALID", err.Error())
		return
	}
	if err := u.linker.Switch(u.cfg.BaseDir, filepath.Join("releases", req.TargetVersion)); err != nil {
		u.fail(req, previous, "CONFIG_INVALID", err.Error())
		return
	}
	if err := u.restart.Restart(ctx, u.cfg.ServiceName); err != nil {
		u.fail(req, previous, "KERNEL_START_FAILED", err.Error())
		return
	}
	if u.healthCheck != nil {
		if err := u.healthCheck(ctx); err != nil {
			u.fail(req, previous, "KERNEL_UNHEALTHY", err.Error())
			return
		}
	}
	u.mu.Lock()
	u.state.State = "succeeded"
	u.state.Progress = 100
	u.state.UpdatedAt = time.Now().UTC()
	u.persistLocked()
	u.mu.Unlock()
}

func (u *Updater) set(req Request, state string, progress int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.OperationID = req.OperationID
	u.state.TargetVersion = req.TargetVersion
	u.state.State = state
	u.state.Progress = progress
	u.state.UpdatedAt = time.Now().UTC()
	u.persistLocked()
}

func (u *Updater) fail(req Request, previous, code, message string) {
	if previous != "" {
		_ = u.linker.Switch(u.cfg.BaseDir, previous)
		_ = u.restart.Restart(context.Background(), u.cfg.ServiceName)
	}
	u.mu.Lock()
	u.state.OperationID = req.OperationID
	u.state.TargetVersion = req.TargetVersion
	u.state.ErrorCode = code
	u.state.ErrorMessage = message
	u.state.UpdatedAt = time.Now().UTC()
	if previous != "" {
		u.state.State = "rolled_back"
	} else {
		u.state.State = "failed"
	}
	u.persistLocked()
	u.mu.Unlock()
}

func (u *Updater) persistLocked() {
	if u.store != nil {
		_ = u.store.Save(u.state)
	}
}

func isRunning(state string) bool {
	switch state {
	case "pending", "downloading", "verifying", "applying":
		return true
	default:
		return false
	}
}

func verifySignature(digest []byte, signatureHex string, publicKeysHex []string) error {
	sig, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature encoding")
	}
	for _, keyHex := range publicKeysHex {
		key, err := hex.DecodeString(strings.TrimSpace(keyHex))
		if err != nil || len(key) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(key), digest, sig) {
			return nil
		}
	}
	return fmt.Errorf("signature verification failed")
}

// HTTPSDownloader 拉取 HTTPS 资产（≤200MB）。
type HTTPSDownloader struct{ Client *http.Client }

func (d HTTPSDownloader) Get(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("update source must be HTTPS")
	}
	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// SystemctlRestarter 通过 systemctl 重启 Agent 服务。
type SystemctlRestarter struct{}

func (SystemctlRestarter) Restart(ctx context.Context, service string) error {
	return runCommand(ctx, "systemctl", "restart", service)
}

// SymlinkLinker 原子切换 current 软链。
type SymlinkLinker struct{}

func (SymlinkLinker) Current(baseDir string) (string, error) {
	target, err := os.Readlink(filepath.Join(baseDir, "current"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return target, nil
}

func (SymlinkLinker) Switch(baseDir, target string) error {
	current := filepath.Join(baseDir, "current")
	tmp := filepath.Join(baseDir, fmt.Sprintf(".current.tmp.%d", time.Now().UnixNano()))
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, current)
}

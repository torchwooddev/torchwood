package functions

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// zip 解压限制（§5.4 防 zip 炸弹）。
const (
	maxZipEntries       = 1000
	maxZipEntryBytes    = 100 << 20 // 单条 ≤ 100 MiB
	maxZipTotalBytes    = 200 << 20 // 总解压 ≤ 200 MiB
	maxBuildLogBytes    = 64 << 10  // 构建日志截断 64KB
	maxContainerOutSize = 1 << 20   // stdout/stderr 缓冲上限（结果在 app 层再截断 64KB）
	// maxExecEnvBudgetBytes 是 data + env 合并预算（execve 32KiB 单参数硬限制）。
	maxExecEnvBudgetBytes = 32 << 10
)

// specResources 是资源规格 → 容器配额映射（与 app 层 runtimes 表一致，
// infra 不依赖 app 包，自持一份兜底）。
var specResources = map[string]struct {
	cpu    float64
	memory int64
}{
	"shared-1x": {cpu: 0.5, memory: 256 << 20},
	"shared-2x": {cpu: 1.0, memory: 512 << 20},
}

// dockerExecutor 是真实 Docker 执行器：Build（zip → 镜像）+ Execute（run 容器）。
type dockerExecutor struct {
	cfg *config.AppConfig
	cli *client.Client

	// initErr 是 client 构造失败的错误（配置错误延迟到首次调用暴露）。
	initErr error

	// netMu/netReady 保护网络就绪状态：仅缓存成功，失败不缓存（可重试）。
	netMu    sync.Mutex
	netReady bool
}

// NewDockerExecutor creates a Docker-based functions executor.
func NewDockerExecutor(cfg *config.AppConfig) functions.Executor {
	d := &dockerExecutor{cfg: cfg}
	host := cfg.GetFunctions().GetDocker().GetHost()
	// WithAPIVersionNegotiation：与 daemon 协商 API 版本，避免客户端默认
	// 版本高于 daemon（如 CI runner 上 daemon 1.48 vs 客户端 1.51）导致
	// "client version is too new" 构建失败。
	cli, err := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		d.initErr = fmt.Errorf("create docker client: %w", err)
		return d
	}
	d.cli = cli
	return d
}

// client 返回 docker client；构造失败时返回 initErr。
func (d *dockerExecutor) client() (*client.Client, error) {
	if d.cli == nil {
		return nil, d.initErr
	}
	return d.cli, nil
}

func (d *dockerExecutor) imageName(functionID, deploymentID string) string {
	registry := d.cfg.GetFunctions().GetDocker().GetRegistry()
	if registry == "" {
		registry = "torchwood-funcs"
	}
	return fmt.Sprintf("%s/func-%s-%s", registry, functionID, deploymentID)
}

// ensureNetwork 检查配置的 bridge 网络存在，不存在则创建（幂等；失败不缓存）。
func (d *dockerExecutor) ensureNetwork(ctx context.Context) error {
	cli, err := d.client()
	if err != nil {
		return err
	}
	name := d.cfg.GetFunctions().GetDocker().GetNetwork()
	if name == "" {
		return nil
	}
	d.netMu.Lock()
	defer d.netMu.Unlock()
	if d.netReady {
		return nil
	}
	if _, err := cli.NetworkInspect(ctx, name, network.InspectOptions{}); err == nil {
		d.netReady = true
		return nil
	}
	if _, createErr := cli.NetworkCreate(ctx, name, network.CreateOptions{Driver: "bridge"}); createErr != nil {
		// 创建失败但网络可能已被并发创建。
		if _, inspectErr := cli.NetworkInspect(ctx, name, network.InspectOptions{}); inspectErr == nil {
			d.netReady = true
			return nil
		}
		return fmt.Errorf("ensure network %q: %w", name, createErr)
	}
	d.netReady = true
	return nil
}

// Build 将 zip 代码包解压校验后构建为镜像 {registry}/func-{functionID}-{deploymentID}。
func (d *dockerExecutor) Build(ctx context.Context, functionID, deploymentID, zipPath string) error {
	cli, err := d.client()
	if err != nil {
		return err
	}

	buildDir, err := os.MkdirTemp("", "torchwood-build-*")
	if err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	runtime, err := extractZip(zipPath, buildDir)
	if err != nil {
		return err
	}
	dockerfile, err := dockerfileFor(runtime)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}

	tarCtx, err := tarDir(buildDir)
	if err != nil {
		return fmt.Errorf("tar build context: %w", err)
	}
	opts := build.ImageBuildOptions{
		Tags:       []string{d.imageName(functionID, deploymentID)},
		Dockerfile: "Dockerfile",
		Remove:     true,
	}
	resp, err := cli.ImageBuild(ctx, tarCtx, opts)
	if err != nil {
		// daemon 侧构建失败时错误消息包含构建日志尾部。
		return fmt.Errorf("docker build failed: %s", truncateLog(err.Error()))
	}
	defer resp.Body.Close()
	// 读取构建输出（保留尾部 64KB）并扫描流内 {"error":...} JSON：
	// BuildKit 模式下构建失败不返回 Go error，只在流末尾携带 error 消息。
	log, buildErr := readBuildOutput(resp.Body)
	if buildErr != nil {
		return buildError(buildErr, log)
	}
	return nil
}

// Execute 运行构建产物镜像（安全基线 + TW_DATA 环境变量注入 + 超时清理）。
func (d *dockerExecutor) Execute(ctx context.Context, exec functions.Execution) (*functions.ExecutionResult, error) {
	if exec.DeploymentID == "" {
		return nil, status.Error(codes.InvalidArgument, "deployment id is required")
	}
	cli, err := d.client()
	if err != nil {
		return nil, err
	}
	if err := d.ensureNetwork(ctx); err != nil {
		return nil, err
	}

	// execve 32KiB 单参数硬限制：data + env 合并预算兜底（app 层已校验）。
	budget := len(exec.Data)
	for k, v := range exec.Env {
		budget += len(k) + len(v)
	}
	if budget > maxExecEnvBudgetBytes {
		return nil, status.Errorf(codes.InvalidArgument, "data and environment variables exceed combined maximum of %d bytes", maxExecEnvBudgetBytes)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeoutFromExec(exec))
	defer cancel()

	res := specResources[exec.Spec]
	if res.cpu <= 0 {
		res = specResources["shared-1x"]
	}
	networkName := d.cfg.GetFunctions().GetDocker().GetNetwork()
	if networkName == "" {
		networkName = "none"
	}
	stopTimeout := 5
	env := []string{"TW_DATA=" + exec.Data}
	for k, v := range exec.Env {
		env = append(env, k+"="+v)
	}

	cfg := &container.Config{
		Image:       d.imageName(exec.FunctionID, exec.DeploymentID),
		Env:         env,
		StopTimeout: &stopTimeout,
	}
	hostCfg := &container.HostConfig{
		NetworkMode:    container.NetworkMode(networkName),
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
		ReadonlyRootfs: true,
		Tmpfs:          map[string]string{"/tmp": ""},
		Resources: container.Resources{
			Memory:    res.memory,
			NanoCPUs:  int64(res.cpu * 1e9),
			PidsLimit: int64Ptr(512),
		},
	}

	created, err := cli.ContainerCreate(runCtx, cfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}
	containerID := created.ID
	remove := func() {
		_ = cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{Force: true})
	}
	defer remove()

	attach, err := cli.ContainerAttach(runCtx, containerID, container.AttachOptions{Stream: true, Stdout: true, Stderr: true})
	if err != nil {
		return nil, fmt.Errorf("attach container: %w", err)
	}
	defer attach.Close()

	if err := cli.ContainerStart(runCtx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	// 流式收集 stdout/stderr（stdcopy 解复用）。
	var stdoutBuf, stderrBuf limitedBuffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attach.Reader)
	}()

	started := time.Now()
	waitCh, errCh := cli.ContainerWait(runCtx, containerID, container.WaitConditionNotRunning)
	select {
	case <-runCtx.Done():
		// 超时或调用方取消：停止并强制清理容器（无残留）。
		_ = cli.ContainerStop(context.Background(), containerID, container.StopOptions{})
		<-done
		return nil, runCtx.Err()
	case err := <-errCh:
		<-done
		return nil, fmt.Errorf("wait container: %w", err)
	case wait := <-waitCh:
		if wait.Error != nil {
			<-done
			return nil, fmt.Errorf("container exited with error: %s", truncateLog(wait.Error.Message))
		}
		<-done
		statusCode := int(wait.StatusCode)

		stdout := stdoutBuf.String()
		stderr := stderrBuf.String()
		result := &functions.ExecutionResult{
			StatusCode: statusCode,
			Stdout:     stdout,
			Stderr:     stderr,
			Response:   parseResponse(stdout),
			DurationMS: time.Since(started).Milliseconds(),
		}
		return result, nil
	}
}

// RemoveImage 删除构建产物镜像（幂等）。
func (d *dockerExecutor) RemoveImage(ctx context.Context, functionID, deploymentID string) error {
	cli, err := d.client()
	if err != nil {
		return err
	}
	_, err = cli.ImageRemove(ctx, d.imageName(functionID, deploymentID), image.RemoveOptions{})
	if client.IsErrNotFound(err) {
		return nil
	}
	return err
}

// extractZip 解压 zip 到 destDir（防 zip 炸弹与路径穿越），返回 runtime ID。
func extractZip(zipPath, destDir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, "invalid zip file")
	}
	defer zr.Close()

	if len(zr.File) > maxZipEntries {
		return "", status.Errorf(codes.InvalidArgument, "zip contains too many entries (max %d)", maxZipEntries)
	}
	var total uint64
	hasIndexJS := false
	hasMainPy := false
	root := filepath.Clean(destDir)

	for _, f := range zr.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return "", status.Error(codes.InvalidArgument, "zip entry is a symlink")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > maxZipEntryBytes {
			return "", status.Errorf(codes.InvalidArgument, "zip entry %q exceeds %d bytes", f.Name, maxZipEntryBytes)
		}
		total += f.UncompressedSize64
		if total > maxZipTotalBytes {
			return "", status.Errorf(codes.InvalidArgument, "zip total uncompressed size exceeds %d bytes", maxZipTotalBytes)
		}

		// zip slip：Clean 后必须仍位于解压根目录内。
		name := filepath.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return "", status.Errorf(codes.InvalidArgument, "zip entry %q escapes root", f.Name)
		}
		target := filepath.Join(root, name)
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return "", status.Errorf(codes.InvalidArgument, "zip entry %q escapes root", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("create zip entry dir: %w", err)
		}
		src, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return "", fmt.Errorf("write zip entry %q: %w", f.Name, err)
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return "", fmt.Errorf("extract zip entry %q: %w", f.Name, copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close zip entry %q: %w", f.Name, closeErr)
		}

		switch name {
		case "index.js":
			hasIndexJS = true
		case "main.py":
			hasMainPy = true
		}
	}

	switch {
	case hasIndexJS:
		return "node-18.0", nil
	case hasMainPy:
		return "python-3.11", nil
	default:
		return "", status.Error(codes.InvalidArgument, "missing entrypoint file: expected index.js (node) or main.py (python)")
	}
}

// dockerfileFor 生成运行时 Dockerfile（data 经 TW_DATA 环境变量传递，禁止拼接进命令）。
func dockerfileFor(runtime string) (string, error) {
	switch runtime {
	case "node-18.0":
		return "FROM node:18-alpine\n" +
			"WORKDIR /app\n" +
			"COPY . .\n" +
			"USER node\n" +
			`CMD ["node","-e","const {main}=require('./index');Promise.resolve(main(JSON.parse(process.env.TW_DATA||'{}'))).then(r=>console.log(JSON.stringify(r))).catch(e=>{console.error(e);process.exit(1)})"]` + "\n", nil
	case "python-3.11":
		return "FROM python:3.11-alpine\n" +
			"WORKDIR /app\n" +
			"COPY . .\n" +
			"USER 1000\n" +
			`CMD ["python","-c","import json,os,main;r=main.main(json.loads(os.environ.get('TW_DATA','{}')));print(json.dumps(r))"]` + "\n", nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "unsupported runtime %q", runtime)
	}
}

// tarDir 将目录打包为 docker build context 的 tar 流。
func tarDir(dir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !d.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, f)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

// parseResponse 取 stdout 末行为合法 JSON 则原样返回，否则空串。
func parseResponse(stdout string) string {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if json.Valid([]byte(last)) {
		return last
	}
	return ""
}

// readBuildOutput 逐行读取 docker build 输出流，保留尾部 maxBuildLogBytes 字节，
// 并扫描 `{"error":...}` / `{"errorDetail":{"message":...}}` JSON（BuildKit 失败
// 消息位于流末尾，不产生 Go error）。返回 (日志尾部, 构建错误)。
func readBuildOutput(r io.Reader) (string, error) {
	var log tailBuffer
	var buildErr error
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		log.Write(append(line, '\n'))
		var msg struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Error != "" {
			buildErr = errors.New(msg.Error)
			break
		}
		if msg.ErrorDetail.Message != "" {
			buildErr = errors.New(msg.ErrorDetail.Message)
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return log.String(), err
	}
	return log.String(), buildErr
}

// buildError 组合构建失败错误：错误消息在前，日志尾部按总预算 64KB 裁剪在后。
func buildError(buildErr error, log string) error {
	msg := truncateLog(buildErr.Error())
	header := "docker build failed: " + msg + "\nbuild log tail:\n"
	room := maxBuildLogBytes - len(header)
	if room < 0 {
		room = 0
	}
	if len(log) > room {
		log = log[len(log)-room:]
	}
	return errors.New(header + log)
}

func truncateLog(s string) string {
	if len(s) <= maxBuildLogBytes {
		return s
	}
	return s[:maxBuildLogBytes]
}

func timeoutFromExec(exec functions.Execution) time.Duration {
	if exec.Timeout <= 0 {
		return 15 * time.Second
	}
	return time.Duration(exec.Timeout) * time.Second
}

func int64Ptr(v int64) *int64 { return &v }

// limitedBuffer 是带上限的 bytes.Buffer（防止恶意输出耗尽内存）。
type limitedBuffer struct {
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > maxContainerOutSize {
		remaining := maxContainerOutSize - b.buf.Len()
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// tailBuffer 仅保留最后 maxBuildLogBytes 字节（构建失败原因通常在输出末尾）。
type tailBuffer struct {
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.buf = append(b.buf, p...)
	if len(b.buf) > maxBuildLogBytes {
		drop := len(b.buf) - maxBuildLogBytes
		copy(b.buf, b.buf[drop:])
		b.buf = b.buf[:maxBuildLogBytes]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.buf) }

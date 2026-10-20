package tunnel

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const boreVersion = "v0.6.0"

var (
	ansi       = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	remotePort = regexp.MustCompile(`remote_port=(\d+)`)
)

func (m *Manager) boreExe() string {
	name := "bore"
	if runtime.GOOS == "windows" {
		name = "bore.exe"
	}
	return filepath.Join(m.opt.DataDir, "bin", name)
}

// ensureBore downloads the bore binary once (Windows; elsewhere it must be on PATH).
func (m *Manager) ensureBore(ctx context.Context) (string, error) {
	exe := m.boreExe()
	if _, err := os.Stat(exe); err == nil {
		return exe, nil
	}
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("bore"); err == nil {
			return p, nil
		}
		return "", errors.New("bore otomatis hanya untuk Windows. Install manual: https://github.com/ekzhang/bore")
	}
	arch := "x86_64"
	if runtime.GOARCH == "386" {
		arch = "i686"
	}
	url := fmt.Sprintf("https://github.com/ekzhang/bore/releases/download/%s/bore-%s-%s-pc-windows-msvc.zip", boreVersion, boreVersion, arch)
	m.opt.Logf("info", "Download bore %s (hanya sekali)…", boreVersion)

	dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gagal download bore: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gagal download bore: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.Base(f.Name), "bore.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			return "", err
		}
		out, err := os.Create(exe)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return "", err
		}
		return exe, out.Close()
	}
	return "", errors.New("bore.exe tidak ada di zip")
}

// openBore runs `bore local <DEVICE_PORT> --to bore.pub` and reads the public port.
func (m *Manager) openBore(ctx context.Context) (*active, error) {
	exe, err := m.ensureBore(ctx)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(pctx, exe, "local", strconv.Itoa(m.opt.DevicePort), "--to", m.opt.BoreHost)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "RUST_LOG=info")
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	portCh := make(chan int, 1)
	var tail strings.Builder
	go func() {
		sc := bufio.NewScanner(stdout)
		found := false
		for sc.Scan() {
			line := ansi.ReplaceAllString(sc.Text(), "")
			if !found {
				tail.WriteString(line + "\n")
				if mm := remotePort.FindStringSubmatch(line); mm != nil {
					p, _ := strconv.Atoi(mm[1])
					found = true
					portCh <- p
				}
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case p := <-portCh:
		return &active{
			info: &Info{Host: m.opt.BoreHost, Port: p, Provider: "bore"},
			close: func() {
				cancel()
				<-done
			},
			done: done,
		}, nil
	case <-done:
		cancel()
		return nil, fmt.Errorf("bore berhenti: %s", strings.TrimSpace(tail.String()))
	case <-time.After(openTimeout):
		cancel()
		<-done
		return nil, errors.New("bore tidak mendapat port dalam 20 detik")
	}
}

package printer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type RTSPSnapshotOptions struct {
	URL         string
	OutputPath  string
	Format      string
	JPEGQuality int
	Transport   string
	Keyframe    bool
	Timeout     time.Duration
}

func BuildRTSPSURL(ip, accessCode, username string) (string, error) {
	ip = strings.TrimSpace(ip)
	accessCode = strings.TrimSpace(accessCode)
	username = strings.TrimSpace(username)
	if ip == "" {
		return "", errors.New("missing printer IP")
	}
	if accessCode == "" {
		return "", errors.New("missing access code")
	}
	if username == "" {
		username = "bblp"
	}

	u := &url.URL{
		Scheme: "rtsps",
		User:   url.UserPassword(username, accessCode),
		Host:   ip,
		Path:   "/streaming/live/1",
	}
	return u.String(), nil
}

func ResolveFFmpegPath(explicitPath string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	if envPath := strings.TrimSpace(os.Getenv("BAMBU_FFMPEG")); envPath != "" {
		return envPath, nil
	}
	if bundledPath := bundledFFmpegPath(); bundledPath != "" {
		if info, err := os.Stat(bundledPath); err == nil && !info.IsDir() {
			return bundledPath, nil
		}
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}
	return "", errors.New("ffmpeg not found; install ffmpeg, set BAMBU_FFMPEG, or install Bambu Studio cameratools")
}

func SnapshotRTSPS(ffmpegPath string, opts RTSPSnapshotOptions) ([]byte, error) {
	if strings.TrimSpace(ffmpegPath) == "" {
		return nil, errors.New("missing ffmpeg path")
	}
	if strings.TrimSpace(opts.URL) == "" {
		return nil, errors.New("missing RTSPS URL")
	}
	format, err := normalizeSnapshotFormat(opts.Format, opts.OutputPath)
	if err != nil {
		return nil, err
	}
	transport := normalizeTransport(opts.Transport)
	if transport == "" {
		return nil, errors.New("invalid transport; use tcp or udp")
	}
	quality := opts.JPEGQuality
	if quality == 0 {
		quality = 1
	}
	if quality < 1 || quality > 31 {
		return nil, errors.New("jpeg quality must be between 1 and 31")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	args := buildRTSPSnapshotArgs(opts.URL, format, transport, quality, opts.Keyframe)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("ffmpeg timed out after %s", timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg failed: %s", msg)
	}
	if stdout.Len() == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "ffmpeg returned no image data"
		}
		return nil, errors.New(msg)
	}

	return stdout.Bytes(), nil
}

func buildRTSPSnapshotArgs(streamURL, format, transport string, jpegQuality int, keyframe bool) []string {
	args := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", transport,
		"-i", streamURL,
	}
	if keyframe {
		args = append(args, "-vf", "select='eq(pict_type,I)'", "-fps_mode", "passthrough")
	}
	args = append(args, "-frames:v", "1", "-f", "image2pipe")
	switch format {
	case "png":
		args = append(args, "-vcodec", "png")
	default:
		args = append(args, "-vcodec", "mjpeg", "-q:v", strconv.Itoa(jpegQuality))
	}
	args = append(args, "-")
	return args
}

func normalizeSnapshotFormat(format, outPath string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(format))
	if value == "" {
		switch strings.ToLower(filepath.Ext(outPath)) {
		case ".png":
			return "png", nil
		case ".jpg", ".jpeg":
			return "jpg", nil
		}
		return "jpg", nil
	}
	switch value {
	case "jpg", "jpeg":
		return "jpg", nil
	case "png":
		return "png", nil
	default:
		return "", errors.New("invalid format; use jpg or png")
	}
}

func normalizeTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "tcp":
		return "tcp"
	case "udp":
		return "udp"
	default:
		return ""
	}
}

func bundledFFmpegPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(configDir, "BambuStudio", "cameratools", name)
}

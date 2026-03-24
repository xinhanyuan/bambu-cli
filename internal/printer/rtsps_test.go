package printer

import (
	"strings"
	"testing"
)

func TestBuildRTSPSURL(t *testing.T) {
	got, err := BuildRTSPSURL("192.168.3.162", "abc123", "")
	if err != nil {
		t.Fatalf("BuildRTSPSURL returned error: %v", err)
	}
	want := "rtsps://bblp:abc123@192.168.3.162/streaming/live/1"
	if got != want {
		t.Fatalf("BuildRTSPSURL = %q, want %q", got, want)
	}
}

func TestBuildRTSPSURLPercentEncodesCredentials(t *testing.T) {
	got, err := BuildRTSPSURL("printer.local", "ab:c@1", "user")
	if err != nil {
		t.Fatalf("BuildRTSPSURL returned error: %v", err)
	}
	if !strings.Contains(got, "user:ab%3Ac%401@printer.local") {
		t.Fatalf("BuildRTSPSURL did not encode credentials: %q", got)
	}
}

func TestBuildRTSPSnapshotArgs(t *testing.T) {
	args := buildRTSPSnapshotArgs("rtsps://example", "png", "tcp", 1, true)
	joined := strings.Join(args, " ")
	for _, part := range []string{
		"-rtsp_transport tcp",
		"-vf select='eq(pict_type,I)'",
		"-fps_mode passthrough",
		"-vcodec png",
		"-f image2pipe",
	} {
		if !strings.Contains(joined, part) {
			t.Fatalf("args %q missing %q", joined, part)
		}
	}
}

func TestNormalizeSnapshotFormat(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		outPath string
		want    string
	}{
		{name: "explicit jpg", format: "jpeg", outPath: "frame.png", want: "jpg"},
		{name: "png from extension", format: "", outPath: "frame.png", want: "png"},
		{name: "default jpg", format: "", outPath: "frame.bin", want: "jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSnapshotFormat(tt.format, tt.outPath)
			if err != nil {
				t.Fatalf("normalizeSnapshotFormat returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeSnapshotFormat = %q, want %q", got, tt.want)
			}
		})
	}
}

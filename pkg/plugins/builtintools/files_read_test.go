package builtintools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coren/pkg/llm"
	"coren/pkg/modelinfo"
	"coren/pkg/tools"
)

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakePNG is a minimal valid-enough PNG header for type detection.
var fakePNG = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32))

func TestReadTextFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", []byte("hello world"))
	result, err := (ReadFile{Root: dir}).RunResult(context.Background(), `{"path":"a.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello world" {
		t.Errorf("text = %q", result.Text)
	}
	if len(result.Parts) != 0 {
		t.Errorf("text file should not produce media parts: %+v", result.Parts)
	}
}

func TestReadImageAttachesWhenModelSupports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pic.png", fakePNG)

	ctx := llm.WithModelInfo(context.Background(), modelinfo.Info{
		ID:         "vision-model",
		Modalities: modelinfo.Modalities{Input: []string{"text", "image"}},
	})
	result, err := (ReadFile{Root: dir}).RunResult(ctx, `{"path":"pic.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("parts = %+v, want 1 image", result.Parts)
	}
	part := result.Parts[0]
	if part.Type != llm.PartImage || part.MimeType != "image/png" {
		t.Errorf("part = %+v", part)
	}
	if _, err := base64.StdEncoding.DecodeString(part.Data); err != nil {
		t.Errorf("part data is not valid base64: %v", err)
	}
}

func TestReadImageDegradesWhenModelLacksVision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pic.png", fakePNG)

	ctx := llm.WithModelInfo(context.Background(), modelinfo.Info{
		ID:         "text-model",
		Modalities: modelinfo.Modalities{Input: []string{"text"}},
	})
	result, err := (ReadFile{Root: dir}).RunResult(ctx, `{"path":"pic.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 0 {
		t.Errorf("non-vision model must not receive media: %+v", result.Parts)
	}
	if !strings.Contains(result.Text, "image/png") || !strings.Contains(result.Text, "does not support image") {
		t.Errorf("degradation message = %q", result.Text)
	}
}

func TestReadAudioDegradesWithoutSupport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sound.mp3", []byte("ID3"+strings.Repeat("a", 30)))

	ctx := llm.WithModelInfo(context.Background(), modelinfo.Info{
		ID:         "text-only",
		Modalities: modelinfo.Modalities{Input: []string{"text"}},
	})
	result, _ := (ReadFile{Root: dir}).RunResult(ctx, `{"path":"sound.mp3"}`)
	if len(result.Parts) != 0 || !strings.Contains(result.Text, "audio") {
		t.Errorf("audio degradation = %q", result.Text)
	}
}

func TestReadBinaryReportsTypeWithoutParts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "blob.bin", []byte{0x00, 0x01, 0x02, 0x03})

	result, err := (ReadFile{Root: dir}).RunResult(context.Background(), `{"path":"blob.bin"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 0 {
		t.Errorf("binary should not attach: %+v", result.Parts)
	}
	if !strings.Contains(result.Text, "cannot be shown") {
		t.Errorf("binary message = %q", result.Text)
	}
}

func TestReadOversizedMediaIsNotAttached(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "big.png", append(fakePNG, make([]byte, 4096)...))

	ctx := llm.WithModelInfo(context.Background(), modelinfo.Info{
		ID:         "vision",
		Modalities: modelinfo.Modalities{Input: []string{"image"}},
	})
	result, _ := (ReadFile{Root: dir, MaxMediaBytes: 64}).RunResult(ctx, `{"path":"big.png"}`)
	if len(result.Parts) != 0 {
		t.Errorf("oversized media must not attach: %+v", result.Parts)
	}
	if !strings.Contains(result.Text, "exceeds the inline limit") {
		t.Errorf("oversize message = %q", result.Text)
	}
}

func TestReadFileImplementsResultTool(t *testing.T) {
	var _ tools.ResultTool = ReadFile{}
}

func TestClassifyByContentWhenExtensionUnknown(t *testing.T) {
	kind, mime := classify("noext", fakePNG)
	if kind != kindImage || mime != "image/png" {
		t.Errorf("classify = %v, %q", kind, mime)
	}
}

package builtintools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"coren/pkg/llm"
	"coren/pkg/modelinfo"
	"coren/pkg/tools"
)

// mediaKind maps a broad file category to a content part type.
type mediaKind string

const (
	kindText  mediaKind = "text"
	kindImage mediaKind = "image"
	kindAudio mediaKind = "audio"
	kindVideo mediaKind = "video"
	kindOther mediaKind = "other"
)

// ReadFile reads a file. Text is returned as text; media (image/audio/video) is
// returned as content parts when the active model supports that modality, and
// otherwise reported by type so the model learns it exists but cannot be viewed.
type ReadFile struct {
	// Root, when set, restricts reads to this directory tree.
	Root string
	// MaxMediaBytes caps inline media size; zero uses a 20 MiB default.
	MaxMediaBytes int
}

func (t ReadFile) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "read_file",
		Description: "Read a file by path. Text files return their contents. " +
			"Images, audio, and video are returned as viewable content when the current model " +
			"supports that modality; otherwise the file type is reported instead.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path to read."},
			},
			"required": []string{"path"},
		},
	}
}

func (t ReadFile) Run(ctx context.Context, arguments string) (string, error) {
	result, err := t.RunResult(ctx, arguments)
	return result.Text, err
}

func (t ReadFile) RunResult(ctx context.Context, arguments string) (tools.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := resolvePath(t.Root, args.Path)
	if err != nil {
		return tools.Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{}, err
	}

	kind, mimeType := classify(path, data)
	switch kind {
	case kindText:
		return tools.Result{Text: string(data)}, nil
	case kindImage, kindAudio, kindVideo:
		return t.handleMedia(ctx, path, kind, mimeType, data)
	default:
		return tools.Result{Text: fmt.Sprintf(
			"%s is a %s file (%d bytes); it is not text and cannot be shown as an attachment.",
			path, mimeOr(mimeType, "binary"), len(data))}, nil
	}
}

// handleMedia returns media parts when the model supports the modality, else an
// explanatory message including why it could not be viewed.
func (t ReadFile) handleMedia(ctx context.Context, path string, kind mediaKind, mimeType string, data []byte) (tools.Result, error) {
	info, _ := modelInfoFromContext(ctx)

	limit := t.MaxMediaBytes
	if limit <= 0 {
		limit = 20 << 20
	}
	if len(data) > limit {
		return tools.Result{Text: fmt.Sprintf(
			"%s is a %s file of %d bytes, which exceeds the inline limit of %d bytes; it was not attached.",
			path, mimeType, len(data), limit)}, nil
	}

	if !modelSupports(info, string(kind)) {
		return tools.Result{Text: fmt.Sprintf(
			"%s is a %s file, but the current model does not support %s input, so it cannot be viewed. "+
				"Tell the user what the file is rather than guessing its contents.",
			path, mimeType, kind)}, nil
	}

	part := llm.ContentPart{
		Type:     string(kind),
		MimeType: mimeType,
		Data:     base64.StdEncoding.EncodeToString(data),
	}
	return tools.Result{
		Text:  fmt.Sprintf("Attached %s (%s, %d bytes).", path, mimeType, len(data)),
		Parts: []llm.ContentPart{part},
	}, nil
}

// modelInfoFromContext extracts the active model's capabilities, if attached.
func modelInfoFromContext(ctx context.Context) (modelinfo.Info, bool) {
	raw, ok := llm.ModelInfoFrom(ctx)
	if !ok {
		return modelinfo.Info{}, false
	}
	info, ok := raw.(modelinfo.Info)
	return info, ok
}

// modelSupports reports whether the model accepts a given input modality. An
// unknown model (no info) is assumed to support images only, the common case.
func modelSupports(info modelinfo.Info, modality string) bool {
	if info.ID == "" {
		return modality == string(kindImage)
	}
	return info.SupportsInput(modality)
}

// classify determines a file's category and MIME type from extension and content.
func classify(path string, data []byte) (mediaKind, string) {
	if mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); mimeType != "" {
		return kindForMIME(mimeType), stripParams(mimeType)
	}
	detected := detectMIME(data)
	return kindForMIME(detected), detected
}

func kindForMIME(mimeType string) mediaKind {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return kindImage
	case strings.HasPrefix(mimeType, "audio/"):
		return kindAudio
	case strings.HasPrefix(mimeType, "video/"):
		return kindVideo
	case strings.HasPrefix(mimeType, "text/"),
		mimeType == "application/json",
		mimeType == "application/xml",
		mimeType == "application/javascript":
		return kindText
	default:
		return kindOther
	}
}

// detectMIME sniffs common media signatures when the extension is unhelpful.
func detectMIME(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && string(data[:3]) == "\xff\xd8\xff":
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 4 && string(data[:4]) == "RIFF" && len(data) >= 12 && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 4 && string(data[:4]) == "%PDF":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

func stripParams(mimeType string) string {
	if i := strings.Index(mimeType, ";"); i >= 0 {
		return strings.TrimSpace(mimeType[:i])
	}
	return mimeType
}

func mimeOr(mimeType, fallback string) string {
	if mimeType == "" || mimeType == "application/octet-stream" {
		return fallback
	}
	return mimeType
}

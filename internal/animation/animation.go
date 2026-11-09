package animation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const maxOutput = 32 << 20

// FromVideo converts an MP4 or WebM animation to a looping GIF bounded to 512 pixels.
func FromVideo(ctx context.Context, data []byte) ([]byte, error) {
	return fromVideo(ctx, data, 512)
}

// EmojiFromVideo converts a video emoji to a 64-pixel GIF.
func EmojiFromVideo(ctx context.Context, data []byte) ([]byte, error) {
	return fromVideo(ctx, data, 64)
}

func fromVideo(ctx context.Context, data []byte, size int) (result []byte, err error) {
	input, err := os.CreateTemp("", "zefxi-animation-*")
	if err != nil {
		return nil, fmt.Errorf("create video input: %w", err)
	}
	defer func() {
		err = errors.Join(err, os.Remove(input.Name()))
	}()
	_, writeErr := input.Write(data)
	closeErr := input.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, fmt.Errorf("write video input: %w", err)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	// FFmpeg's native VP9 decoder drops the alpha plane of Telegram WebM stickers.
	if bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		args = append(args, "-c:v", "libvpx-vp9")
	}
	args = append(args, "-i", input.Name(), "-filter_complex",
		fmt.Sprintf("fps=20,scale=%d:%d:force_original_aspect_ratio=decrease:flags=lanczos,split[a][b];[a]palettegen=reserve_transparent=1[p];[b][p]paletteuse=alpha_threshold=128", size, size),
		"-an", "-gifflags", "-offsetting", "-loop", "0", "-f", "gif", "pipe:1")
	return convert(ctx, nil, "ffmpeg", args...)
}

// FromTGS converts a gzip-compressed Telegram Lottie sticker to a 512x512 GIF.
func FromTGS(ctx context.Context, data []byte) ([]byte, error) {
	return convert(ctx, data, "lottieconverter", "-", "-", "gif", "512x512", "20")
}

// EmojiFromTGS converts a Telegram Lottie emoji to a 64x64 GIF.
func EmojiFromTGS(ctx context.Context, data []byte) ([]byte, error) {
	return convert(ctx, data, "lottieconverter", "-", "-", "gif", "64x64", "20")
}

// EmojiFromWebP converts a static Telegram emoji to a 64x64 PNG.
func EmojiFromWebP(ctx context.Context, data []byte) ([]byte, error) {
	return convert(ctx, data, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0", "-vf", "scale=64:64:flags=lanczos", "-frames:v", "1",
		"-f", "image2pipe", "-vcodec", "png", "pipe:1")
}

func convert(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	output := limitedBuffer{limit: maxOutput}
	diagnostic := limitedBuffer{limit: 8 << 10, truncate: true}
	cmd.Stdout = &output
	cmd.Stderr = &diagnostic
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, fmt.Errorf("%s conversion: %w: %s", name, err, strings.TrimSpace(diagnostic.buffer.String()))
	}
	return output.buffer.Bytes(), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	truncate bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if len(p) <= remaining {
		return b.buffer.Write(p)
	}
	_, _ = b.buffer.Write(p[:remaining])
	if b.truncate {
		return len(p), nil
	}
	return remaining, fmt.Errorf("animation output exceeds %d bytes: %w", b.limit, io.ErrShortWrite)
}

// StickerPNG converts a static image to a Telegram-sized WebP sticker.
func StickerPNG(ctx context.Context, data []byte) ([]byte, error) {
	output, err := convert(ctx, data, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0",
		"-vf", "scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos", "-frames:v", "1",
		"-c:v", "libwebp", "-f", "webp", "pipe:1")
	if err == nil && len(output) > 512*1024 {
		return nil, fmt.Errorf("static sticker exceeds 512 KB")
	}
	return output, err
}

// StickerVideo converts an animation to a Telegram VP9 video sticker.
func StickerVideo(ctx context.Context, data []byte, lottie bool) ([]byte, error) {
	if lottie {
		var err error
		data, err = convert(ctx, data, "lottieconverter", "-", "-", "gif", "512x512", "30")
		if err != nil {
			return nil, err
		}
	}
	output, err := convert(ctx, data, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0",
		"-t", "3", "-vf", "fps=30,scale=512:512:force_original_aspect_ratio=decrease:force_divisible_by=2:flags=lanczos,format=yuva420p",
		"-an", "-c:v", "libvpx-vp9", "-b:v", "450k", "-maxrate", "500k", "-bufsize", "1M",
		"-f", "webm", "pipe:1")
	if err == nil && len(output) > 256*1024 {
		return nil, fmt.Errorf("video sticker exceeds 256 KB")
	}
	return output, err
}

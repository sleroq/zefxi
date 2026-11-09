package animation

import (
	"bytes"
	"compress/gzip"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func requireConverter(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s unavailable: %v", name, err)
	}
}

func checkGIF(t *testing.T, data []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("GIF89a")) && !bytes.HasPrefix(data, []byte("GIF87a")) {
		t.Fatal("output is not a GIF")
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) < 2 {
		t.Fatalf("expected multiple frames, got %d", len(animation.Image))
	}
	if animation.Config.Width > 512 || animation.Config.Height > 512 || animation.LoopCount != 0 {
		t.Fatalf("unexpected GIF dimensions or looping: %dx%d, loop %d",
			animation.Config.Width, animation.Config.Height, animation.LoopCount)
	}
}

func TestFromVideo(t *testing.T) {
	requireConverter(t, "ffmpeg")

	for _, format := range []struct{ name, codec string }{{"mp4", "mpeg4"}, {"webm", "libvpx-vp9"}} {
		t.Run(format.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			source := "testsrc2=size=96x64:rate=20"
			if format.name == "webm" {
				source = "color=red@0.0:size=96x64:rate=20,format=yuva420p,drawbox=x=20:y=20:w=20:h=20:color=red@1:t=fill:replace=1"
			}
			args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", source, "-t", "0.5", "-c:v", format.codec}
			if format.name == "mp4" {
				args = append(args, "-movflags", "frag_keyframe+empty_moov")
			}
			args = append(args, "-f", format.name, "pipe:1")
			cmd := exec.CommandContext(ctx, "ffmpeg", args...)
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			video, err := cmd.Output()
			if err != nil {
				t.Fatalf("generate fixture: %v: %s", err, &diagnostic)
			}

			data, err := FromVideo(ctx, video)
			if err != nil {
				t.Fatal(err)
			}
			checkGIF(t, data)

			if format.name == "webm" {
				animation, err := gif.DecodeAll(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, alpha := animation.Image[0].At(0, 0).RGBA()
				if alpha != 0 {
					t.Fatal("WebM transparency was lost")
				}
				_, _, _, alpha = animation.Image[0].At(150, 150).RGBA()
				if alpha == 0 {
					t.Fatal("WebM opaque content was lost")
				}
			}
		})
	}
}

func TestFromTGS(t *testing.T) {
	requireConverter(t, "lottieconverter")
	fixture, err := os.ReadFile("testdata/sticker.json")
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	writer := gzip.NewWriter(&input)
	if _, err := writer.Write(fixture); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := FromTGS(ctx, input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	checkGIF(t, data)
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if animation.Config.Width != 512 || animation.Config.Height != 512 {
		t.Fatalf("expected 512x512, got %dx%d", animation.Config.Width, animation.Config.Height)
	}
}

func TestOutputLimit(t *testing.T) {
	buffer := limitedBuffer{limit: 3}
	if _, err := io.Copy(&buffer, io.LimitReader(bytes.NewReader([]byte("four")), 4)); err == nil {
		t.Fatal("expected output limit error")
	}
	if buffer.buffer.Len() != 3 {
		t.Fatalf("buffer grew beyond limit: %d", buffer.buffer.Len())
	}
}

func TestTelegramStickers(t *testing.T) {
	requireConverter(t, "ffmpeg")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, format := range []string{"png", "apng", "gif", "lottie"} {
		t.Run(format, func(t *testing.T) {
			var source []byte
			var err error
			switch format {
			case "lottie":
				requireConverter(t, "lottieconverter")
				source, err = os.ReadFile("testdata/sticker.json")
			case "gif":
				frame := image.NewPaletted(image.Rect(0, 0, 96, 64), color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}})
				frame.SetColorIndex(30, 30, 1)
				var input bytes.Buffer
				err = gif.EncodeAll(&input, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}})
				source = input.Bytes()
			default:
				sourceFilter := "color=red@0.0:size=96x64:rate=10,format=rgba,drawbox=x=20:y=20:w=20:h=20:color=red@1:t=fill"
				args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", sourceFilter, "-t", "0.5"}
				if format == "png" {
					args = append(args, "-frames:v", "1", "-f", "image2pipe", "-c:v", "png")
				} else {
					args = append(args, "-f", format)
				}
				args = append(args, "pipe:1")
				source, err = exec.CommandContext(ctx, "ffmpeg", args...).Output()
			}
			if err != nil {
				t.Fatal(err)
			}
			var output []byte
			if format == "png" {
				output, err = StickerPNG(ctx, source)
			} else {
				output, err = StickerVideo(ctx, source, format == "lottie")
			}
			if err != nil {
				t.Fatal(err)
			}
			if format == "png" {
				if !bytes.HasPrefix(output, []byte("RIFF")) || len(output) > 512*1024 {
					t.Fatal("invalid static sticker")
				}
			} else if !bytes.HasPrefix(output, []byte{0x1a, 0x45, 0xdf, 0xa3}) || len(output) > 256*1024 {
				t.Fatal("invalid video sticker")
			}
			probe := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
				"-show_entries", "stream=width,height,codec_name,r_frame_rate", "-of", "default=nw=1", "pipe:0")
			probe.Stdin = bytes.NewReader(output)
			info, err := probe.Output()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(info, []byte("width=512")) && !bytes.Contains(info, []byte("height=512")) {
				t.Fatalf("dimensions: %s", info)
			}
			if format != "png" && (!bytes.Contains(info, []byte("codec_name=vp9")) || !bytes.Contains(info, []byte("r_frame_rate=30/1"))) {
				t.Fatalf("video: %s", info)
			}
			if format == "apng" || format == "gif" {
				decoder := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
					"-c:v", "libvpx-vp9", "-i", "pipe:0", "-frames:v", "1", "-f", "image2pipe", "-c:v", "png", "pipe:1")
				decoder.Stdin = bytes.NewReader(output)
				frame, err := decoder.Output()
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(frame))
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, alpha := img.At(0, 0).RGBA()
				if alpha != 0 {
					t.Fatal("transparency lost")
				}
			}
		})
	}
}

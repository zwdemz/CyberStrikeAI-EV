package vision_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"cyberstrike-ai/internal/vision"
	"golang.org/x/image/tiff"
)

func TestTIFFPreprocessingAndMalformedInput(t *testing.T) {
	source := image.NewPaletted(image.Rect(0, 0, 80, 40), color.Palette{color.Black, color.White})
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			source.SetColorIndex(x, y, uint8((x+y)%2))
		}
	}
	var encoded bytes.Buffer
	if err := tiff.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"valid.tiff", encoded.Bytes(), true},
		{"truncated.tiff", encoded.Bytes()[:12], false},
		{"invalid.tiff", []byte("II\x2a\x00\xff\xff\xff\x7f"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), test.name)
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			payload, meta, err := vision.PreprocessImageFile(path, vision.PreprocessOptions{MaxDimension: 32, MaxPayloadBytes: 10000})
			if !test.valid {
				if err == nil {
					t.Fatal("malformed TIFF accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(payload.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 16 || meta.OutputMIMEType != "image/jpeg" {
				t.Fatalf("unexpected output: %+v", meta)
			}
		})
	}
}

package tools

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"slices"
	"strings"
)

const (
	maxImageSide  = 1600
	maxImageBytes = 1 << 20
)

// clipboardImage picks the image type to read from the clipboard's offered types,
// or "" when it holds plain text (browsers offer both for a copied image's page text).
func clipboardImage(types []string) string {
	for _, t := range types {
		if t == "text/plain" || strings.HasPrefix(t, "text/plain;") || t == "UTF8_STRING" {
			return ""
		}
	}
	for _, want := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if slices.Contains(types, want) {
			return want
		}
	}
	for _, t := range types {
		if strings.HasPrefix(t, "image/") {
			return t
		}
	}
	return ""
}

// shrinkImage re-encodes an image as JPEG when it is large, halving it until it fits maxImageSide.
// Small images are returned as they are.
func shrinkImage(data []byte, mime string) ([]byte, string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if len(data) <= maxImageBytes {
			return data, mime, nil
		}
		return nil, "", fmt.Errorf("the clipboard holds a %s image too large to return", mime)
	}
	b := img.Bounds()
	if len(data) <= maxImageBytes && max(b.Dx(), b.Dy()) <= maxImageSide {
		return data, mime, nil
	}
	step := 1
	for max(b.Dx(), b.Dy())/step > maxImageSide {
		step *= 2
	}
	small := image.NewRGBA(image.Rect(0, 0, b.Dx()/step, b.Dy()/step))
	for y := range small.Rect.Dy() {
		for x := range small.Rect.Dx() {
			small.Set(x, y, img.At(b.Min.X+x*step, b.Min.Y+y*step))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, &jpeg.Options{Quality: 80}); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "image/jpeg", nil
}

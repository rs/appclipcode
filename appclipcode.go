// Package appclipcode generates Apple App Clip Code SVGs.
//
// It reimplements Apple's AppClipCodeGenerator tool in pure Go, producing
// bit-identical output using the same multi-context Huffman URL compression,
// Reed-Solomon error correction, and circular fingerprint SVG rendering.
//
// Basic usage:
//
//	svg, err := appclipcode.GenerateWithTemplate("https://example.com", 0, nil)
//
// Custom colors:
//
//	svg, err := appclipcode.Generate("https://example.com", "000000", "FFFFFF", nil)
//
// Reading a URL back from an SVG:
//
//	url, err := appclipcode.ReadSVG(svgBytes)
package appclipcode

import (
	"fmt"

	"github.com/rs/appclipcode/internal/codec"
	appscan "github.com/rs/appclipcode/internal/scan"
	appsvg "github.com/rs/appclipcode/internal/svg"
)

// CodeType is the type of App Clip Code.
type CodeType string

const (
	CodeTypeCamera CodeType = "cam"
	CodeTypeNFC    CodeType = "nfc"
)

// Options configures App Clip Code generation.
type Options struct {
	Type CodeType // "cam" (default) or "nfc"
}

type GaloisField = codec.GaloisField
type RSEncoder = codec.RSEncoder

func (o *Options) defaults() {
	if o.Type == "" {
		o.Type = CodeTypeCamera
	}
}

func NewGF(primitive, size, genBase int) *GaloisField {
	return codec.NewGF(primitive, size, genBase)
}

func NewRSEncoder(gf *GaloisField, numParity int) *RSEncoder {
	return codec.NewRSEncoder(gf, numParity)
}

// Generate creates an App Clip Code SVG for the given URL with the specified colors.
// Colors are 6-digit hex RGB strings (e.g. "FF3B30").
// The third color is looked up from the preset palette if the fg/bg combination
// matches a known template, otherwise it is computed as the midpoint.
func Generate(rawURL, foreground, background string, opts *Options) ([]byte, error) {
	if opts == nil {
		opts = &Options{}
	}
	opts.defaults()

	fg, err := ParseHexColor(foreground)
	if err != nil {
		return nil, fmt.Errorf("foreground: %w", err)
	}
	bg, err := ParseHexColor(background)
	if err != nil {
		return nil, fmt.Errorf("background: %w", err)
	}

	third := findThirdColor(fg, bg)
	pal := Palette{Foreground: fg, Background: bg, Third: third}

	return generateWithPalette(rawURL, pal, opts)
}

// GenerateWithTemplate creates an App Clip Code SVG using a predefined color template (0-17).
func GenerateWithTemplate(rawURL string, templateIndex int, opts *Options) ([]byte, error) {
	if opts == nil {
		opts = &Options{}
	}
	opts.defaults()

	pal, err := TemplateByIndex(templateIndex)
	if err != nil {
		return nil, err
	}

	return generateWithPalette(rawURL, pal, opts)
}

func generateWithPalette(rawURL string, pal Palette, opts *Options) ([]byte, error) {
	// Step 1: Compress URL to 16 bytes
	compressed, err := CompressURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("compress URL: %w", err)
	}

	// Step 2: Encode payload to 185 bits
	allBits, err := EncodePayload(compressed)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}

	// Step 3: Render SVG
	svg := RenderSVG(allBits, pal, rawURL, opts.Type)
	return svg, nil
}

func CompressURL(rawURL string) ([]byte, error) {
	return codec.CompressURL(rawURL)
}

func EncodePayload(payload []byte) ([]bool, error) {
	return codec.EncodePayload(payload)
}

func RenderSVG(bits []bool, pal Palette, url string, codeType CodeType) []byte {
	return appsvg.RenderSVG(bits, svgPalette(pal), url, appsvg.CodeType(codeType))
}

func ReadSVG(svgData []byte) (string, error) {
	return appsvg.ReadSVG(svgData)
}

func ReadImage(data []byte) (string, error) {
	return appscan.ReadImage(data)
}

func DecodePayload(bits []bool) ([]byte, error) {
	return codec.DecodePayload(bits)
}

func DecompressURL(payload []byte) (string, error) {
	return codec.DecompressURL(payload)
}

// findThirdColor looks up or computes the third color for a fg/bg combination.
func findThirdColor(fg, bg Color) Color {
	// Check against known presets
	for _, p := range basePalettes {
		if p.Foreground == fg && p.Background == bg {
			return p.Third
		}
		if p.Foreground == bg && p.Background == fg {
			return p.Third
		}
	}

	// Fallback: compute midpoint between fg and bg
	return Color{
		R: uint8((int(fg.R) + int(bg.R)) / 2),
		G: uint8((int(fg.G) + int(bg.G)) / 2),
		B: uint8((int(fg.B) + int(bg.B)) / 2),
	}
}

func svgPalette(p Palette) appsvg.Palette {
	return appsvg.Palette{
		Foreground: appsvg.Color{R: p.Foreground.R, G: p.Foreground.G, B: p.Foreground.B},
		Background: appsvg.Color{R: p.Background.R, G: p.Background.G, B: p.Background.B},
		Third:      appsvg.Color{R: p.Third.R, G: p.Third.G, B: p.Third.B},
	}
}

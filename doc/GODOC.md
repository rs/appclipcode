# Go Documentation

`appclipcode` is the Go implementation in this repository.

It supports:

- App Clip Code SVG generation
- template and custom-color rendering
- camera and NFC variants
- SVG decoding
- raster image decoding

## Install

Library:

```bash
go get github.com/rs/appclipcode
```

CLI:

```bash
go install github.com/rs/appclipcode/cmd/appclipcodegen@latest
```

## Quick Start

Generate an SVG:

```bash
appclipcodegen gen https://example.com -o code.svg
```

Scan a code back to a URL:

```bash
appclipcodegen scan code.svg
```

List the built-in visual templates:

```bash
appclipcodegen templates
```

## Library Usage

Generate an SVG with a predefined template:

```go
package main

import (
	"os"

	"github.com/rs/appclipcode"
)

func main() {
	svg, err := appclipcode.GenerateWithTemplate("https://example.com", 0, nil)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("code.svg", svg, 0o644); err != nil {
		panic(err)
	}
}
```

Generate with custom colors:

```go
svg, err := appclipcode.Generate("https://example.com", "FFFFFF", "000000", nil)
```

Use NFC mode:

```go
svg, err := appclipcode.GenerateWithTemplate(
	"https://example.com",
	4,
	&appclipcode.Options{
		Type: appclipcode.CodeTypeNFC,
	},
)
```

Decode from SVG:

```go
url, err := appclipcode.ReadSVG(svgBytes)
```

Decode from an image:

```go
url, err := appclipcode.ReadImage(imageBytes)
```

## CLI Usage

Generate an SVG:

```bash
appclipcodegen gen https://example.com -output code.svg
```

Generate with custom colors:

```bash
appclipcodegen gen https://example.com -fg FFFFFF -bg 000000 -o code.svg
```

Generate an NFC code:

```bash
appclipcodegen gen https://example.com -index 4 -type nfc -o code.svg
```

List templates:

```bash
appclipcodegen templates
```

Scan a code back to a URL:

```bash
appclipcodegen scan code.svg
```

## Public API

Core generation functions:

- `Generate(rawURL, foreground, background string, opts *Options)`
- `GenerateWithTemplate(rawURL string, templateIndex int, opts *Options)`
- `CompressURL(rawURL string)`
- `EncodePayload(payload []byte)`
- `RenderSVG(bits []bool, pal Palette, url string, codeType CodeType)`

Decoding functions:

- `ReadSVG(svgData []byte)`
- `ReadImage(data []byte)`
- `DecodePayload(bits []bool)`
- `DecompressURL(payload []byte)`

Palette helpers:

- `Templates()`
- `TemplateByIndex(index int)`
- `ParseHexColor(s string)`

Low-level Reed-Solomon helpers:

- `NewGF(primitive, size, genBase int)`
- `NewRSEncoder(gf *GaloisField, numParity int)`

## Notes

The Go implementation is the most complete implementation in this repository.
It includes both generation and decoding paths, including raster-image scanning.

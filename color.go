package appclipcode

import (
	"fmt"
	"strings"
)

// Color represents an RGB color.
type Color struct {
	R, G, B uint8
}

// ParseHexColor parses a 6-digit hex color string like "FF3B30".
func ParseHexColor(s string) (Color, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return Color{}, fmt.Errorf("color must be 6 hex digits, got %q", s)
	}
	var c Color
	_, err := fmt.Sscanf(s, "%02x%02x%02x", &c.R, &c.G, &c.B)
	if err != nil {
		return Color{}, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	return c, nil
}

// Hex returns the color as a lowercase 6-digit hex string with # prefix.
func (c Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// Palette holds the three colors used in an App Clip Code.
type Palette struct {
	Foreground Color // data-color=0 arcs
	Background Color // background circle
	Third      Color // data-color=1 arcs
}

// Template is a predefined color template.
type Template struct {
	Index      int
	Foreground Color
	Background Color
	Third      Color
}

// 9 base palette presets. Each has fg-on-bg (even index) and bg-on-fg (odd index) variants.
var basePalettes = [9]Palette{
	{Foreground: Color{0x00, 0x00, 0x00}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0x88, 0x88, 0x88}},
	{Foreground: Color{0x77, 0x77, 0x77}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0xAA, 0xAA, 0xAA}},
	{Foreground: Color{0xFF, 0x3B, 0x30}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0xFF, 0x99, 0x99}},
	{Foreground: Color{0xEE, 0x77, 0x33}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0xEE, 0xBB, 0x88}},
	{Foreground: Color{0x33, 0xAA, 0x22}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0x99, 0xDD, 0x99}},
	{Foreground: Color{0x00, 0xA6, 0xA1}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0x88, 0xDD, 0xCC}},
	{Foreground: Color{0x00, 0x7A, 0xFF}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0x77, 0xBB, 0xFF}},
	{Foreground: Color{0x58, 0x56, 0xD6}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0xBB, 0xBB, 0xEE}},
	{Foreground: Color{0xCC, 0x73, 0xE1}, Background: Color{0xFF, 0xFF, 0xFF}, Third: Color{0xEE, 0xBB, 0xEE}},
}

// Templates returns all 18 predefined color templates.
func Templates() []Template {
	templates := make([]Template, 18)
	for i, p := range basePalettes {
		// Even index: white foreground on colored background
		templates[i*2] = Template{
			Index:      i * 2,
			Foreground: Color{0xFF, 0xFF, 0xFF},
			Background: p.Foreground,
			Third:      p.Third,
		}
		// Odd index: colored foreground on white background
		templates[i*2+1] = Template{
			Index:      i*2 + 1,
			Foreground: p.Foreground,
			Background: p.Background,
			Third:      p.Third,
		}
	}
	return templates
}

// TemplateByIndex returns the palette for a given template index (0-17).
func TemplateByIndex(index int) (Palette, error) {
	templates := Templates()
	if index < 0 || index >= len(templates) {
		return Palette{}, fmt.Errorf("template index must be 0-17, got %d", index)
	}
	t := templates[index]
	return Palette{
		Foreground: t.Foreground,
		Background: t.Background,
		Third:      t.Third,
	}, nil
}

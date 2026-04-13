package svg

import (
	"fmt"
	"math"
	"strings"
)

type Color struct {
	R, G, B, A uint8
}

func (c Color) Hex() string {
	if c.A == 0xFF {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}

	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

type Palette struct {
	Foreground Color
	Background Color
	Third      Color
}

// Ring layout constants from reverse-engineered binary.
var (
	RingRadii     = [5]float64{177.2016, 224.1012, 271.0008, 317.9004, 364.8}
	RingRotations = [5]float64{-78, -85, -70, -63, -70}
	RingBitCounts = [5]int{17, 23, 26, 29, 33} // total = 128
	RingGapAngles = [5]float64{7.5, 5.6, 5.0, 4.2, 3.5}

	ringRadii     = RingRadii
	ringRotations = RingRotations
	ringBitCounts = RingBitCounts
	ringGapAngles = RingGapAngles
)

const (
	CenterX     = 400.0
	CenterY     = 400.0
	BgRadius    = 400.0
	StrokeWidth = 23.5
	Deg2Rad     = math.Pi / 180.0

	centerX     = CenterX
	centerY     = CenterY
	bgRadius    = BgRadius
	strokeWidth = StrokeWidth
	deg2rad     = Deg2Rad
)

// CodeType is the type of App Clip Code.
type CodeType string

const (
	CodeTypeCamera CodeType = "cam"
	CodeTypeNFC    CodeType = "nfc"
)

// RenderSVG generates an App Clip Code SVG from 185 bits and a color palette.
func RenderSVG(bits []bool, pal Palette, url string, codeType CodeType) []byte {
	var sb strings.Builder

	sb.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	fmt.Fprintf(&sb, `<svg data-design="Fingerprint" data-payload="%s" viewBox="0 0 800 800" xmlns="http://www.w3.org/2000/svg">`+"\n", escapeXML(url))
	sb.WriteString("    <title>App Clip Code</title>\n")

	// Background circle
	fmt.Fprintf(&sb, `    <circle cx="%.6f" cy="%.6f" id="Background" r="%.6f" style="fill:%s"/>`+"\n",
		centerX, centerY, bgRadius, pal.Background.Hex())

	// Markers (5 rings)
	sb.WriteString(`    <g id="Markers">` + "\n")

	// Rendering model:
	// - Bits 0-127 (LUT-permuted): gap bits per ring position.
	//     0 = VISIBLE (arc drawn), 1 = INVISIBLE (gap, no arc).
	// - Bits 128+ : color stream for visible positions.
	//     Assigned sequentially to positions with gap bit = 0 across all rings.
	//     0 = foreground color, 1 = third color.
	gapBits := bits[:128]
	colorStream := bits[128:] // starts at what was called "separator"

	gapOffset := 0
	colorIdx := 0
	for ring := 0; ring < 5; ring++ {
		n := ringBitCounts[ring]
		ringGap := gapBits[gapOffset : gapOffset+n]
		gapOffset += n

		// Build per-position state: -1 = invisible, 0 = foreground, 1 = third color
		posState := make([]int, n)
		for i := 0; i < n; i++ {
			if !ringGap[i] { // gap bit 0 = visible
				color := 0
				if colorIdx < len(colorStream) && colorStream[colorIdx] {
					color = 1
				}
				posState[i] = color
				colorIdx++
			} else {
				posState[i] = -1 // invisible
			}
		}

		fmt.Fprintf(&sb, `        <g name="ring-%d" transform="rotate(%.0f %.0f %.0f)">`+"\n",
			ring+1, ringRotations[ring], centerX, centerY)

		writeRingArcsFromState(&sb, ring, posState, pal)

		sb.WriteString("        </g>\n")
	}
	sb.WriteString("    </g>\n")

	// Center logo
	writeLogo(&sb, codeType, pal)

	sb.WriteString("</svg>\n")

	return []byte(sb.String())
}

// writeRingArcsFromState renders arcs for a ring using per-position state.
// State: -1 = invisible gap, 0 = foreground arc, 1 = third-color arc.
// Consecutive same-color visible positions merge into one arc.
func writeRingArcsFromState(sb *strings.Builder, ringIdx int, posState []int, pal Palette) {
	n := ringBitCounts[ringIdx]
	radius := ringRadii[ringIdx]
	bitAngle := 360.0 / float64(n)
	gapAngle := ringGapAngles[ringIdx]

	type arc struct {
		dataColor int
		startBit  int
		count     int
	}

	// Each visible position gets an arc that extends RIGHT into adjacent
	// invisible positions (absorbing them) until hitting another visible position.
	// The last arc wraps around if trailing positions are invisible.
	var arcs []arc
	for i := 0; i < n; i++ {
		if posState[i] == -1 {
			continue
		}
		span := 1
		for i+span < n && posState[i+span] == -1 {
			span++
		}
		// If this is the last visible position and there are invisible positions
		// wrapping around to the start, extend the span to cover them.
		if i+span == n {
			for j := 0; j < n && posState[j] == -1; j++ {
				span++
			}
		}
		arcs = append(arcs, arc{dataColor: posState[i], startBit: i, count: span})
	}

	for _, a := range arcs {
		startAngle := float64(a.startBit)*bitAngle + gapAngle
		endAngle := float64(a.startBit+a.count)*bitAngle - gapAngle

		sx := centerX + radius*math.Cos(startAngle*deg2rad)
		sy := centerY + radius*math.Sin(startAngle*deg2rad)
		ex := centerX + radius*math.Cos(endAngle*deg2rad)
		ey := centerY + radius*math.Sin(endAngle*deg2rad)

		arcSpan := endAngle - startAngle
		if arcSpan < 0 {
			arcSpan += 360
		}
		largeArc := 0
		if arcSpan > 180.0 {
			largeArc = 1
		}

		strokeColor := pal.Foreground
		if a.dataColor == 1 {
			strokeColor = pal.Third
		}

		fmt.Fprintf(sb, `            <path d="M %f %f A %f %f 0 %d 0 %f %f" data-color="%d" style="fill:none;stroke:%s;stroke-linecap:round;stroke-miterlimit:10;stroke-width:%fpx"/>`+"\n",
			ex, ey, radius, radius, largeArc, sx, sy, a.dataColor, strokeColor.Hex(), strokeWidth)
	}
}

func writeLogo(sb *strings.Builder, codeType CodeType, pal Palette) {
	if codeType == CodeTypeNFC {
		fmt.Fprintf(sb, `    <g id="Logo" data-logo-type="phone" transform="translate(293.400000 293.400000) scale(1.980000 1.980000)">`+"\n")
		fmt.Fprintf(sb, `        <path id="outer_circle" d="%s" style="fill:%s"/>`+"\n", phoneOuterPath, pal.Foreground.Hex())
		fmt.Fprintf(sb, `        <path id="phone_screen" d="%s" style="fill:%s;isolation:isolate"/>`+"\n", phoneScreenPath, pal.Third.Hex())
	} else {
		fmt.Fprintf(sb, `    <g id="Logo" data-logo-type="Camera" transform="translate(293.275699 293.275699) scale(1.874000 1.874000)">`+"\n")
		for _, p := range cameraLogoPaths {
			fmt.Fprintf(sb, `        <path d="%s" style="fill:%s"/>`+"\n", p, pal.Foreground.Hex())
		}
	}
	sb.WriteString("    </g>\n")
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

package svg

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/appclipcode/internal/codec"
)

// ReadSVG extracts the encoded URL from an App Clip Code SVG.
// It parses the ring arc segments, decodes the Reed-Solomon error correction,
// unscrambles the payload, and decompresses the URL.
func ReadSVG(svgData []byte) (string, error) {
	bits, err := ExtractBits(string(svgData))
	if err != nil {
		return "", fmt.Errorf("extract bits: %w", err)
	}

	payload, err := codec.DecodePayload(bits)
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	url, err := codec.DecompressURL(payload)
	if err != nil {
		return "", fmt.Errorf("decompress URL: %w", err)
	}

	return url, nil
}

// ExtractBits parses an App Clip Code SVG and reconstructs the codec's
// output bit vector: 128 gap bits + color stream bits.
//
// Gap bit model: 0 = VISIBLE arc position, 1 = INVISIBLE gap.
// Color stream: sequential color assignments (0=foreground, 1=third) for visible positions.
func ExtractBits(svg string) ([]bool, error) {
	cx, cy := 400.0, 400.0

	pathRe := regexp.MustCompile(
		`d="M\s+([\d.]+)\s+([\d.]+)\s+A\s+[\d.]+\s+[\d.]+\s+0\s+\d\s+0\s+` +
			`([\d.]+)\s+([\d.]+)"[^>]*data-color="(\d)"`)

	gapBits := make([]bool, 128)
	var colorBits []bool
	offset := 0

	for i := range gapBits {
		gapBits[i] = true
	}

	for ri := 0; ri < 5; ri++ {
		r := ringRadii[ri]
		n := ringBitCounts[ri]
		ba := 360.0 / float64(n)
		hg := ringGapAngles[ri]

		type pt struct{ x, y float64 }
		starts := make([]pt, n)
		ends := make([]pt, n)
		for i := 0; i < n; i++ {
			a0 := (float64(i)*ba + hg) * deg2rad
			a1 := (float64(i+1)*ba - hg) * deg2rad
			starts[i] = pt{cx + r*math.Cos(a0), cy + r*math.Sin(a0)}
			ends[i] = pt{cx + r*math.Cos(a1), cy + r*math.Sin(a1)}
		}

		snap := func(px, py float64, pts []pt) int {
			best, bestD := 0, math.MaxFloat64
			for i, p := range pts {
				d := (px-p.x)*(px-p.x) + (py-p.y)*(py-p.y)
				if d < bestD {
					bestD = d
					best = i
				}
			}
			return best
		}

		ringTag := fmt.Sprintf(`name="ring-%d"`, ri+1)
		idx := strings.Index(svg, ringTag)
		if idx < 0 {
			return nil, fmt.Errorf("ring %d not found in SVG", ri+1)
		}
		end := strings.Index(svg[idx:], "</g>")
		if end < 0 {
			return nil, fmt.Errorf("ring %d closing tag not found", ri+1)
		}
		chunk := svg[idx : idx+end]

		type arcInfo struct {
			startPos int
			color    int
		}
		var ringArcs []arcInfo

		for _, m := range pathRe.FindAllStringSubmatch(chunk, -1) {
			mx, _ := strconv.ParseFloat(m[1], 64)
			my, _ := strconv.ParseFloat(m[2], 64)
			ax, _ := strconv.ParseFloat(m[3], 64)
			ay, _ := strconv.ParseFloat(m[4], 64)
			color, _ := strconv.Atoi(m[5])

			_ = mx
			_ = my
			startPos := snap(ax, ay, starts)
			ringArcs = append(ringArcs, arcInfo{startPos: startPos, color: color})
		}

		for _, a := range ringArcs {
			gapBits[offset+a.startPos] = false
			colorBits = append(colorBits, a.color == 1)
		}

		offset += n
	}

	result := make([]bool, 128+len(colorBits))
	copy(result, gapBits)
	copy(result[128:], colorBits)

	return result, nil
}

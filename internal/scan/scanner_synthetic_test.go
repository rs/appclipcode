package scan

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"testing"

	"github.com/rs/appclipcode/internal/codec"
	appsvg "github.com/rs/appclipcode/internal/svg"
)

func TestReadImageRasterSynthetic(t *testing.T) {
	urls := []string{
		"https://example.com",
		"https://www.apple.com",
		"https://appclip.example.com",
	}

	scenarios := []struct {
		name     string
		width    int
		height   int
		diameter int
		rotDeg   float64
		scaleX   float64
		scaleY   float64
		shearX   float64
		shearY   float64
		centerX  float64
		centerY  float64
		format   string
		quality  int
	}{
		{
			name:     "centered-png",
			width:    1000,
			height:   1000,
			diameter: 900,
			rotDeg:   0,
			scaleX:   0.78,
			scaleY:   0.78,
			centerX:  0.5,
			centerY:  0.5,
			format:   "png",
		},
		{
			name:     "rotated-png",
			width:    1320,
			height:   980,
			diameter: 920,
			rotDeg:   29,
			scaleX:   0.60,
			scaleY:   0.60,
			centerX:  0.62,
			centerY:  0.44,
			format:   "png",
		},
		{
			name:     "scaled-png",
			width:    1440,
			height:   1100,
			diameter: 940,
			rotDeg:   0,
			scaleX:   0.46,
			scaleY:   0.46,
			centerX:  0.34,
			centerY:  0.63,
			format:   "png",
		},
	}

	for ui, url := range urls {
		for si, scenario := range scenarios {
			template := 1
			name := scenario.name + "-" + url
			t.Run(name, func(t *testing.T) {
				imageData := buildSyntheticRasterCase(t, url, template, scenario, int64(97+ui*31+si*17))
				got, err := ReadImage(imageData)
				if err != nil {
					t.Fatalf("ReadImage(%s): %v", name, err)
				}
				if got != url {
					t.Fatalf("ReadImage(%s): got %q, want %q", name, got, url)
				}
			})
		}
	}
}

func buildSyntheticRasterCase(t *testing.T, url string, template int, scenario struct {
	name     string
	width    int
	height   int
	diameter int
	rotDeg   float64
	scaleX   float64
	scaleY   float64
	shearX   float64
	shearY   float64
	centerX  float64
	centerY  float64
	format   string
	quality  int
}, seed int64) []byte {
	t.Helper()

	code := renderRasterCode(t, url, template, scenario.diameter)
	bg := makeValidationBackground(scenario.width, scenario.height, seed)
	dst := cloneNRGBA(bg)

	transform := translateAffine(
		scenario.centerX*float64(scenario.width),
		scenario.centerY*float64(scenario.height),
	).
		mul(rotateAffine(scenario.rotDeg * deg2rad)).
		mul(shearAffine(scenario.shearX, scenario.shearY)).
		mul(scaleAffine(scenario.scaleX, scenario.scaleY)).
		mul(translateAffine(-float64(code.Bounds().Dx())/2, -float64(code.Bounds().Dy())/2))

	warpCompositeAffine(dst, code, transform)

	var buf bytes.Buffer
	switch scenario.format {
	case "jpeg":
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: scenario.quality}); err != nil {
			t.Fatalf("encode jpeg: %v", err)
		}
	default:
		if err := png.Encode(&buf, dst); err != nil {
			t.Fatalf("encode png: %v", err)
		}
	}
	return buf.Bytes()
}

func renderRasterCode(t *testing.T, url string, template, diameter int) *image.NRGBA {
	t.Helper()

	compressed, err := codec.CompressURL(url)
	if err != nil {
		t.Fatalf("compress %s: %v", url, err)
	}
	bits, err := codec.EncodePayload(compressed)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	pal, err := testTemplatePalette(template)
	if err != nil {
		t.Fatalf("template %d: %v", template, err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, diameter, diameter))
	scale := float64(diameter) / (2 * bgRadius)
	cx := float64(diameter) / 2
	cy := float64(diameter) / 2

	bgCol := color.NRGBA{R: pal.Background.R, G: pal.Background.G, B: pal.Background.B, A: pal.Background.A}
	drawFilledCircle(img, cx, cy, bgRadius*scale, bgCol)

	posState := buildPosState(bits)
	fgCol := color.NRGBA{R: pal.Foreground.R, G: pal.Foreground.G, B: pal.Foreground.B, A: pal.Foreground.A}
	thirdCol := color.NRGBA{R: pal.Third.R, G: pal.Third.G, B: pal.Third.B, A: pal.Third.A}

	offset := 0
	for ringIndex := 0; ringIndex < len(ringBitCounts); ringIndex++ {
		n := ringBitCounts[ringIndex]
		radius := ringRadii[ringIndex] * scale
		stroke := strokeWidth * scale
		bitAngle := 360.0 / float64(n)
		baseAngle := ringRotations[ringIndex]

		for i := 0; i < n; i++ {
			state := posState[offset+i]
			if state < 0 {
				continue
			}
			col := fgCol
			if state == 1 {
				col = thirdCol
			}

			span := 1
			for j := i + 1; j < n; j++ {
				if posState[offset+j] >= 0 {
					break
				}
				span++
			}
			if i+span == n {
				for j := 0; j < n && posState[offset+j] == -1; j++ {
					span++
				}
			}

			startAngle := baseAngle + float64(i)*bitAngle + ringGapAngles[ringIndex]
			endAngle := baseAngle + float64(i+span)*bitAngle - ringGapAngles[ringIndex]
			drawArc(img, cx, cy, radius, stroke, startAngle, endAngle, col)
		}
		offset += n
	}

	return img
}

func buildPosState(bits []bool) []int {
	state := make([]int, 128)
	for i := range state {
		state[i] = -1
	}

	colorIndex := 129
	for i := 0; i < 128; i++ {
		if bits[i] {
			continue
		}
		if colorIndex < len(bits) && bits[colorIndex] {
			state[i] = 1
		} else {
			state[i] = 0
		}
		colorIndex++
	}
	return state
}

func drawArc(img *image.NRGBA, cx, cy, radius, strokeWidth, startDeg, endDeg float64, col color.NRGBA) {
	bounds := img.Bounds()
	halfStroke := strokeWidth / 2
	startRad := startDeg * deg2rad
	endRad := endDeg * deg2rad

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			r := math.Hypot(dx, dy)
			if math.Abs(r-radius) > halfStroke {
				continue
			}

			angle := math.Atan2(dy, dx)
			if angle < 0 {
				angle += 2 * math.Pi
			}
			if angleInRange(angle, startRad, endRad) {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

func angleInRange(angle, start, end float64) bool {
	twoPi := 2 * math.Pi
	angle = math.Mod(angle+twoPi, twoPi)
	start = math.Mod(start+twoPi, twoPi)
	end = math.Mod(end+twoPi, twoPi)

	if start <= end {
		return angle >= start && angle <= end
	}
	return angle >= start || angle <= end
}

func drawFilledCircle(img *image.NRGBA, cx, cy, radius float64, col color.NRGBA) {
	r2 := radius * radius
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dx*dx+dy*dy <= r2 {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

func makeValidationBackground(width, height int, seed int64) *image.NRGBA {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	baseR := 40 + rng.Intn(120)
	baseG := 40 + rng.Intn(120)
	baseB := 40 + rng.Intn(120)
	accentR := 120 + rng.Intn(120)
	accentG := 120 + rng.Intn(120)
	accentB := 120 + rng.Intn(120)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fx := float64(x) / float64(width)
			fy := float64(y) / float64(height)
			wave := 0.5 + 0.5*math.Sin(fx*7.2+fy*5.1+float64(seed%11))
			stripe := 0.5 + 0.5*math.Sin(fx*21.0-fy*13.0)
			noise := rng.Float64()*0.18 - 0.09

			r := clampChannel(float64(baseR)*(1-wave) + float64(accentR)*wave + stripe*22 + noise*255)
			g := clampChannel(float64(baseG)*(1-wave) + float64(accentG)*wave + stripe*14 + noise*255)
			b := clampChannel(float64(baseB)*(1-wave) + float64(accentB)*wave + stripe*18 + noise*255)
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}

	return img
}

func clampChannel(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

type affine2D struct {
	a float64
	b float64
	c float64
	d float64
	e float64
	f float64
}

func translateAffine(tx, ty float64) affine2D {
	return affine2D{a: 1, c: tx, e: 1, f: ty}
}

func scaleAffine(sx, sy float64) affine2D {
	return affine2D{a: sx, e: sy}
}

func rotateAffine(rad float64) affine2D {
	s, c := math.Sin(rad), math.Cos(rad)
	return affine2D{a: c, b: -s, d: s, e: c}
}

func shearAffine(shx, shy float64) affine2D {
	return affine2D{a: 1, b: shx, d: shy, e: 1}
}

func (m affine2D) mul(other affine2D) affine2D {
	return affine2D{
		a: m.a*other.a + m.b*other.d,
		b: m.a*other.b + m.b*other.e,
		c: m.a*other.c + m.b*other.f + m.c,
		d: m.d*other.a + m.e*other.d,
		e: m.d*other.b + m.e*other.e,
		f: m.d*other.c + m.e*other.f + m.f,
	}
}

func (m affine2D) apply(x, y float64) (float64, float64) {
	return m.a*x + m.b*y + m.c, m.d*x + m.e*y + m.f
}

func (m affine2D) invert() (affine2D, bool) {
	det := m.a*m.e - m.b*m.d
	if math.Abs(det) < 1e-12 {
		return affine2D{}, false
	}
	invDet := 1 / det
	return affine2D{
		a: m.e * invDet,
		b: -m.b * invDet,
		c: (m.b*m.f - m.e*m.c) * invDet,
		d: -m.d * invDet,
		e: m.a * invDet,
		f: (m.d*m.c - m.a*m.f) * invDet,
	}, true
}

func warpCompositeAffine(dst, src *image.NRGBA, transform affine2D) {
	inv, ok := transform.invert()
	if !ok {
		return
	}

	srcBounds := src.Bounds()
	dstBounds := dst.Bounds()
	corners := [][2]float64{
		{0, 0},
		{float64(srcBounds.Dx()), 0},
		{0, float64(srcBounds.Dy())},
		{float64(srcBounds.Dx()), float64(srcBounds.Dy())},
	}

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range corners {
		x, y := transform.apply(corner[0], corner[1])
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x)
		maxY = math.Max(maxY, y)
	}

	x0 := maxInt(dstBounds.Min.X, int(math.Floor(minX))-1)
	y0 := maxInt(dstBounds.Min.Y, int(math.Floor(minY))-1)
	x1 := minInt(dstBounds.Max.X, int(math.Ceil(maxX))+1)
	y1 := minInt(dstBounds.Max.Y, int(math.Ceil(maxY))+1)

	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			sx, sy := inv.apply(float64(x)+0.5, float64(y)+0.5)
			srcColor, ok := sampleNRGBAWithAlpha(src, sx-0.5, sy-0.5)
			if !ok || srcColor.A == 0 {
				continue
			}

			dstColor := dst.NRGBAAt(x, y)
			alpha := float64(srcColor.A) / 255.0
			invAlpha := 1 - alpha
			out := color.NRGBA{
				R: uint8(float64(srcColor.R)*alpha + float64(dstColor.R)*invAlpha + 0.5),
				G: uint8(float64(srcColor.G)*alpha + float64(dstColor.G)*invAlpha + 0.5),
				B: uint8(float64(srcColor.B)*alpha + float64(dstColor.B)*invAlpha + 0.5),
				A: 255,
			}
			dst.SetNRGBA(x, y, out)
		}
	}
}

func sampleNRGBAWithAlpha(img *image.NRGBA, x, y float64) (color.NRGBA, bool) {
	bounds := img.Bounds()
	if x < 0 || y < 0 || x > float64(bounds.Dx()-1) || y > float64(bounds.Dy()-1) {
		return color.NRGBA{}, false
	}

	x = clampFloat(x, 0, float64(bounds.Dx()-1))
	y = clampFloat(y, 0, float64(bounds.Dy()-1))
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := minInt(x0+1, bounds.Dx()-1)
	y1 := minInt(y0+1, bounds.Dy()-1)
	tx := x - float64(x0)
	ty := y - float64(y0)

	c00 := img.NRGBAAt(x0, y0)
	c10 := img.NRGBAAt(x1, y0)
	c01 := img.NRGBAAt(x0, y1)
	c11 := img.NRGBAAt(x1, y1)

	top := lerpNRGBA(c00, c10, tx)
	bottom := lerpNRGBA(c01, c11, tx)
	return lerpNRGBA(top, bottom, ty), true
}

func lerpNRGBA(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t + 0.5),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t + 0.5),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t + 0.5),
		A: uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t + 0.5),
	}
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func testTemplatePalette(index int) (appsvg.Palette, error) {
	switch index {
	case 0:
		return appsvg.Palette{
			Foreground: appsvg.Color{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
			Background: appsvg.Color{R: 0x00, G: 0x00, B: 0x00, A: 0xFF},
			Third:      appsvg.Color{R: 0x88, G: 0x88, B: 0x88, A: 0xFF},
		}, nil
	case 1:
		return appsvg.Palette{
			Foreground: appsvg.Color{R: 0x00, G: 0x00, B: 0x00, A: 0xFF},
			Background: appsvg.Color{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
			Third:      appsvg.Color{R: 0x88, G: 0x88, B: 0x88, A: 0xFF},
		}, nil
	default:
		return appsvg.Palette{}, fmt.Errorf("unsupported test template index %d", index)
	}
}

package scan

import (
	"bytes"
	"fmt"
	"image"
	imagedraw "image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sort"

	"github.com/rs/appclipcode/internal/codec"
	appsvg "github.com/rs/appclipcode/internal/svg"
	xdraw "golang.org/x/image/draw"
)

const (
	maxSearchDim           = 320
	minVisiblePositions    = 56
	maxCandidateCount      = 8
	maxRotationCandidates  = 8
	searchRotationStepDeg  = 1.0
	refinedRotationStepDeg = 0.1
)

var templateBits = [8]bool{false, true, false, true, false, true, false, false}

var (
	ringRadii     = appsvg.RingRadii
	ringRotations = appsvg.RingRotations
	ringBitCounts = appsvg.RingBitCounts
	ringGapAngles = appsvg.RingGapAngles
	gapsBitsOrder = codec.GapsBitsOrderLUT
)

const (
	bgRadius    = appsvg.BgRadius
	strokeWidth = appsvg.StrokeWidth
	deg2rad     = appsvg.Deg2Rad
)

// ReadImage decodes the URL from an App Clip Code image.
// It auto-detects PNG/JPEG (raster) vs SVG (vector) by checking for PNG/JPEG
// magic bytes. For SVG input it delegates to ReadSVG. For raster input it
// uses a ring-pattern detector, samples the code in canonical ring
// coordinates, reconstructs the bitmap bitstream, and decodes the payload.
func ReadImage(data []byte) (string, error) {
	if isPNG(data) || isJPEG(data) {
		return readRaster(data)
	}
	return appsvg.ReadSVG(data)
}

func isPNG(data []byte) bool {
	return len(data) > 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
}

func isJPEG(data []byte) bool {
	return len(data) > 2 && data[0] == 0xff && data[1] == 0xd8
}

func readRaster(data []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}

	bitmap := newRasterBitmap(img)
	fullField := newEdgeField(bitmap)
	searchBitmap, searchScale := downscaleBitmap(bitmap, maxSearchDim)
	searchField := newEdgeField(searchBitmap)

	candidates := locateRasterCandidates(searchField)
	if len(candidates) == 0 {
		return "", fmt.Errorf("could not locate App Clip Code")
	}

	for i := range candidates {
		candidates[i].cx /= searchScale
		candidates[i].cy /= searchScale
		candidates[i].scale /= searchScale
		candidates[i] = refineCandidate(fullField, candidates[i])
	}

	type scoredURL struct {
		url   string
		score float64
		hits  int
	}

	bestByURL := make(map[string]scoredURL)
	for _, cand := range candidates {
		for _, hit := range decodeRasterCandidate(bitmap, fullField, cand) {
			current := bestByURL[hit.url]
			current.url = hit.url
			current.hits++
			if current.hits == 1 || hit.score > current.score {
				current.score = hit.score
			}
			bestByURL[hit.url] = current
		}
	}

	if len(bestByURL) == 0 {
		return "", fmt.Errorf("could not decode App Clip Code")
	}

	var ranked []scoredURL
	for _, hit := range bestByURL {
		ranked = append(ranked, hit)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].hits != ranked[j].hits {
			return ranked[i].hits > ranked[j].hits
		}
		return ranked[i].score > ranked[j].score
	})
	return ranked[0].url, nil
}

type rgbColor struct {
	r float64
	g float64
	b float64
}

func (c rgbColor) add(other rgbColor) rgbColor {
	return rgbColor{c.r + other.r, c.g + other.g, c.b + other.b}
}

func (c rgbColor) scale(v float64) rgbColor {
	return rgbColor{c.r * v, c.g * v, c.b * v}
}

func (c rgbColor) distance2(other rgbColor) float64 {
	dr := c.r - other.r
	dg := c.g - other.g
	db := c.b - other.b
	return dr*dr + dg*dg + db*db
}

type vec2 struct {
	x float64
	y float64
}

func (v vec2) scale(s float64) vec2 {
	return vec2{x: v.x * s, y: v.y * s}
}

func (v vec2) add(other vec2) vec2 {
	return vec2{x: v.x + other.x, y: v.y + other.y}
}

func normalizeVec(v vec2) vec2 {
	n := math.Hypot(v.x, v.y)
	if n == 0 {
		return vec2{x: 1}
	}
	return vec2{x: v.x / n, y: v.y / n}
}

type rasterBitmap struct {
	img *image.NRGBA
	w   int
	h   int
}

func newRasterBitmap(img image.Image) *rasterBitmap {
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	imagedraw.Draw(dst, dst.Bounds(), img, b.Min, imagedraw.Src)
	return &rasterBitmap{
		img: dst,
		w:   dst.Bounds().Dx(),
		h:   dst.Bounds().Dy(),
	}
}

func downscaleBitmap(src *rasterBitmap, maxDim int) (*rasterBitmap, float64) {
	maxSide := src.w
	if src.h > maxSide {
		maxSide = src.h
	}
	if maxSide <= maxDim {
		return src, 1.0
	}

	scale := float64(maxDim) / float64(maxSide)
	dstW := maxInt(1, int(math.Round(float64(src.w)*scale)))
	dstH := maxInt(1, int(math.Round(float64(src.h)*scale)))
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src.img, src.img.Bounds(), xdraw.Over, nil)
	return &rasterBitmap{img: dst, w: dstW, h: dstH}, scale
}

func (bm *rasterBitmap) sampleRGB(x, y float64) (rgbColor, bool) {
	if x < 0 || y < 0 || x > float64(bm.w-1) || y > float64(bm.h-1) {
		return rgbColor{}, false
	}
	if bm.w == 1 || bm.h == 1 {
		return bm.pixelColor(0, 0), true
	}

	x = clampFloat(x, 0, float64(bm.w-1))
	y = clampFloat(y, 0, float64(bm.h-1))

	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := minInt(x0+1, bm.w-1)
	y1 := minInt(y0+1, bm.h-1)
	tx := x - float64(x0)
	ty := y - float64(y0)

	c00 := bm.pixelColor(x0, y0)
	c10 := bm.pixelColor(x1, y0)
	c01 := bm.pixelColor(x0, y1)
	c11 := bm.pixelColor(x1, y1)

	top := lerpColor(c00, c10, tx)
	bottom := lerpColor(c01, c11, tx)
	return lerpColor(top, bottom, ty), true
}

func (bm *rasterBitmap) pixelColor(x, y int) rgbColor {
	i := bm.img.PixOffset(x, y)
	p := bm.img.Pix
	return rgbColor{
		r: float64(p[i]) / 255.0,
		g: float64(p[i+1]) / 255.0,
		b: float64(p[i+2]) / 255.0,
	}
}

func lerpColor(a, b rgbColor, t float64) rgbColor {
	return rgbColor{
		r: a.r + (b.r-a.r)*t,
		g: a.g + (b.g-a.g)*t,
		b: a.b + (b.b-a.b)*t,
	}
}

type edgePixel struct {
	x   float64
	y   float64
	nx  float64
	ny  float64
	mag float64
}

type edgeField struct {
	w         int
	h         int
	gray      []float64
	gradX     []float64
	gradY     []float64
	edge      []float64
	threshold float64
	edges     []edgePixel
}

func newEdgeField(bitmap *rasterBitmap) *edgeField {
	w, h := bitmap.w, bitmap.h
	field := &edgeField{
		w:     w,
		h:     h,
		gray:  make([]float64, w*h),
		gradX: make([]float64, w*h),
		gradY: make([]float64, w*h),
		edge:  make([]float64, w*h),
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bitmap.pixelColor(x, y)
			field.gray[y*w+x] = 0.299*c.r + 0.587*c.g + 0.114*c.b
		}
	}

	mags := make([]float64, 0, (w-2)*(h-2))
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := y*w + x
			gx := -field.gray[(y-1)*w+x-1] - 2*field.gray[y*w+x-1] - field.gray[(y+1)*w+x-1] +
				field.gray[(y-1)*w+x+1] + 2*field.gray[y*w+x+1] + field.gray[(y+1)*w+x+1]
			gy := -field.gray[(y-1)*w+x-1] - 2*field.gray[(y-1)*w+x] - field.gray[(y-1)*w+x+1] +
				field.gray[(y+1)*w+x-1] + 2*field.gray[(y+1)*w+x] + field.gray[(y+1)*w+x+1]
			mag := math.Hypot(gx, gy)
			field.gradX[idx] = gx
			field.gradY[idx] = gy
			field.edge[idx] = mag
			if mag > 0 {
				mags = append(mags, mag)
			}
		}
	}

	if len(mags) == 0 {
		return field
	}

	sort.Float64s(mags)
	p90 := mags[int(0.90*float64(len(mags)-1))]
	maxMag := mags[len(mags)-1]
	field.threshold = maxFloat(p90, maxMag*0.18)

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := y*w + x
			mag := field.edge[idx]
			if mag < field.threshold {
				continue
			}
			field.edges = append(field.edges, edgePixel{
				x:   float64(x) + 0.5,
				y:   float64(y) + 0.5,
				nx:  field.gradX[idx] / mag,
				ny:  field.gradY[idx] / mag,
				mag: mag,
			})
		}
	}

	return field
}

func (f *edgeField) sampleEdge(x, y float64) float64 {
	if x < 0 || y < 0 || x > float64(f.w-1) || y > float64(f.h-1) {
		return 0
	}

	x = clampFloat(x, 0, float64(f.w-1))
	y = clampFloat(y, 0, float64(f.h-1))
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := minInt(x0+1, f.w-1)
	y1 := minInt(y0+1, f.h-1)
	tx := x - float64(x0)
	ty := y - float64(y0)

	v00 := f.edge[y0*f.w+x0]
	v10 := f.edge[y0*f.w+x1]
	v01 := f.edge[y1*f.w+x0]
	v11 := f.edge[y1*f.w+x1]

	top := v00 + (v10-v00)*tx
	bottom := v01 + (v11-v01)*tx
	return top + (bottom-top)*ty
}

type rasterCandidate struct {
	cx    float64
	cy    float64
	scale float64
	score float64
}

func locateRasterCandidates(field *edgeField) []rasterCandidate {
	if len(field.edges) == 0 {
		return nil
	}

	minDim := minInt(field.w, field.h)
	minOuterRadius := maxFloat(20, float64(minDim)*0.10)
	maxOuterRadius := float64(minDim) * 0.45
	radiusStep := maxFloat(3, float64(minDim)/96.0)

	var raw []rasterCandidate
	for outerRadius := minOuterRadius; outerRadius <= maxOuterRadius; outerRadius += radiusStep {
		scale := outerRadius / ringRadii[4]
		acc := make([]float64, field.w*field.h)
		radii := expectedEdgeRadii(scale)

		for _, e := range field.edges {
			for _, r := range radii {
				px := int(math.Round(e.x + e.nx*r))
				py := int(math.Round(e.y + e.ny*r))
				if px >= 0 && px < field.w && py >= 0 && py < field.h {
					acc[py*field.w+px] += e.mag
				}

				px = int(math.Round(e.x - e.nx*r))
				py = int(math.Round(e.y - e.ny*r))
				if px >= 0 && px < field.w && py >= 0 && py < field.h {
					acc[py*field.w+px] += e.mag
				}
			}
		}

		for _, peak := range topLocalMaxima(acc, field.w, field.h, 2, 10) {
			raw = append(raw, rasterCandidate{
				cx:    float64(peak.x),
				cy:    float64(peak.y),
				scale: scale,
				score: peak.score,
			})
		}
	}

	sort.Slice(raw, func(i, j int) bool {
		return raw[i].score > raw[j].score
	})

	var candidates []rasterCandidate
	for _, cand := range raw {
		cand = refineCandidate(field, cand)
		if cand.score <= 0 {
			continue
		}
		duplicate := false
		for _, existing := range candidates {
			if math.Hypot(cand.cx-existing.cx, cand.cy-existing.cy) < 18 &&
				math.Abs(cand.scale-existing.scale)/existing.scale < 0.15 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		candidates = append(candidates, cand)
		if len(candidates) == maxCandidateCount {
			break
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	return candidates
}

type localMaximum struct {
	x     int
	y     int
	score float64
}

func topLocalMaxima(acc []float64, w, h, count, radius int) []localMaximum {
	var peaks []localMaximum
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			score := acc[y*w+x]
			if score == 0 {
				continue
			}
			if score < acc[y*w+x-1] || score < acc[y*w+x+1] ||
				score < acc[(y-1)*w+x] || score < acc[(y+1)*w+x] {
				continue
			}
			peaks = append(peaks, localMaximum{x: x, y: y, score: score})
		}
	}

	sort.Slice(peaks, func(i, j int) bool {
		return peaks[i].score > peaks[j].score
	})

	var result []localMaximum
	for _, peak := range peaks {
		tooClose := false
		for _, existing := range result {
			if math.Hypot(float64(peak.x-existing.x), float64(peak.y-existing.y)) < float64(radius) {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		result = append(result, peak)
		if len(result) == count {
			break
		}
	}
	return result
}

func refineCandidate(field *edgeField, cand rasterCandidate) rasterCandidate {
	best := cand
	best.score = circlePatternScore(field, best.cx, best.cy, best.scale)

	for _, centerStep := range []float64{6, 3, 1.5, 0.5} {
		scaleStep := best.scale * 0.04
		improved := true
		for improved {
			improved = false
			for _, dy := range []float64{-centerStep, 0, centerStep} {
				for _, dx := range []float64{-centerStep, 0, centerStep} {
					for _, ds := range []float64{-scaleStep, 0, scaleStep} {
						if dx == 0 && dy == 0 && ds == 0 {
							continue
						}
						next := rasterCandidate{
							cx:    best.cx + dx,
							cy:    best.cy + dy,
							scale: best.scale + ds,
						}
						if next.scale <= 0 {
							continue
						}
						next.score = circlePatternScore(field, next.cx, next.cy, next.scale)
						if next.score > best.score {
							best = next
							improved = true
						}
					}
				}
			}
		}
	}

	return best
}

func circlePatternScore(field *edgeField, cx, cy, scale float64) float64 {
	edgeRadii := expectedEdgeRadii(scale)
	quietRadii := quietZoneRadii(scale)

	edgeEnergy := 0.0
	quietEnergy := 0.0
	samples := 72.0

	for i := 0.0; i < samples; i++ {
		angle := (i / samples) * 2 * math.Pi
		cosA := math.Cos(angle)
		sinA := math.Sin(angle)

		for _, r := range edgeRadii {
			x := cx + cosA*r
			y := cy + sinA*r
			edgeEnergy += field.sampleEdge(x, y)
		}

		for _, r := range quietRadii {
			x := cx + cosA*r
			y := cy + sinA*r
			quietEnergy += field.sampleEdge(x, y)
		}
	}

	edgeEnergy /= float64(len(edgeRadii)) * samples
	quietEnergy /= float64(len(quietRadii)) * samples
	return edgeEnergy - quietEnergy*0.75
}

func expectedEdgeRadii(scale float64) []float64 {
	halfStroke := strokeWidth / 2
	radii := make([]float64, 0, len(ringRadii)*2+1)
	for _, r := range ringRadii {
		radii = append(radii, (r-halfStroke)*scale, (r+halfStroke)*scale)
	}
	radii = append(radii, bgRadius*scale)
	return radii
}

func quietZoneRadii(scale float64) []float64 {
	halfStroke := strokeWidth / 2
	return []float64{
		(ringRadii[0] - strokeWidth) * scale,
		((ringRadii[0] + halfStroke) + (ringRadii[1] - halfStroke)) * 0.5 * scale,
		((ringRadii[1] + halfStroke) + (ringRadii[2] - halfStroke)) * 0.5 * scale,
		((ringRadii[2] + halfStroke) + (ringRadii[3] - halfStroke)) * 0.5 * scale,
		((ringRadii[3] + halfStroke) + (ringRadii[4] - halfStroke)) * 0.5 * scale,
		((ringRadii[4] + halfStroke) + bgRadius) * 0.5 * scale,
	}
}

type scanTransform struct {
	center vec2
	axisX  vec2
	axisY  vec2
	scaleX float64
	scaleY float64
}

func transformFromParams(cx, cy, angleDeg, scaleX, scaleY float64) scanTransform {
	angle := angleDeg * deg2rad
	return scanTransform{
		center: vec2{x: cx, y: cy},
		axisX:  vec2{x: math.Cos(angle), y: math.Sin(angle)},
		axisY:  vec2{x: -math.Sin(angle), y: math.Cos(angle)},
		scaleX: scaleX,
		scaleY: scaleY,
	}
}

func transformAngleDeg(xform scanTransform) float64 {
	return math.Atan2(xform.axisX.y, xform.axisX.x) / deg2rad
}

func estimateAffineTransform(field *edgeField, cand rasterCandidate) scanTransform {
	base := scanTransform{
		center: vec2{x: cand.cx, y: cand.cy},
		axisX:  vec2{x: 1},
		axisY:  vec2{y: 1},
		scaleX: cand.scale,
		scaleY: cand.scale,
	}

	seeds := []scanTransform{base}

	tol := cand.scale * (strokeWidth * 1.2)
	expectedRadius := bgRadius * cand.scale

	selected := make([]edgePixel, 0, 256)
	var sumX, sumY, weight float64
	for _, e := range field.edges {
		r := math.Hypot(e.x-cand.cx, e.y-cand.cy)
		d := math.Abs(r - expectedRadius)
		if d > tol {
			continue
		}
		w := e.mag * (1 - d/tol)
		selected = append(selected, e)
		sumX += e.x * w
		sumY += e.y * w
		weight += w
	}
	if weight == 0 {
		return refineAffineTransform(field, seeds)
	}

	center := vec2{x: sumX / weight, y: sumY / weight}

	var xx, xy, yy float64
	weight = 0
	for _, e := range selected {
		r := math.Hypot(e.x-center.x, e.y-center.y)
		d := math.Abs(r - expectedRadius)
		if d > tol {
			continue
		}
		w := e.mag * (1 - d/tol)
		dx := e.x - center.x
		dy := e.y - center.y
		xx += dx * dx * w
		xy += dx * dy * w
		yy += dy * dy * w
		weight += w
	}
	if weight == 0 {
		seeds = append(seeds, scanTransform{
			center: center,
			axisX:  vec2{x: 1},
			axisY:  vec2{y: 1},
			scaleX: cand.scale,
			scaleY: cand.scale,
		})
		return refineAffineTransform(field, seeds)
	}

	xx /= weight
	xy /= weight
	yy /= weight

	trace := xx + yy
	delta := math.Sqrt(math.Max(0, (xx-yy)*(xx-yy)+4*xy*xy))
	lambda1 := (trace + delta) * 0.5
	lambda2 := (trace - delta) * 0.5
	if lambda1 <= 0 || lambda2 <= 0 {
		seeds = append(seeds, scanTransform{
			center: center,
			axisX:  vec2{x: 1},
			axisY:  vec2{y: 1},
			scaleX: cand.scale,
			scaleY: cand.scale,
		})
		return refineAffineTransform(field, seeds)
	}

	var major vec2
	if math.Abs(xy) > 1e-9 {
		major = normalizeVec(vec2{x: lambda1 - yy, y: xy})
	} else if xx >= yy {
		major = vec2{x: 1}
	} else {
		major = vec2{y: 1}
	}

	minor := vec2{x: -major.y, y: major.x}
	ratio := math.Sqrt(lambda1 / lambda2)
	ratio = clampFloat(ratio, 1.0, 1.35)
	coarse := scanTransform{
		center: center,
		axisX:  major,
		axisY:  minor,
		scaleX: clampFloat(math.Sqrt(2*lambda1)/bgRadius, cand.scale*0.7, cand.scale*1.4),
		scaleY: clampFloat(math.Sqrt(2*lambda2)/bgRadius, cand.scale*0.7, cand.scale*1.4),
	}
	seeds = append(seeds, scanTransform{
		center: center,
		axisX:  vec2{x: 1},
		axisY:  vec2{y: 1},
		scaleX: cand.scale,
		scaleY: cand.scale,
	})
	if ratio >= 1.03 {
		seeds = append(seeds, coarse)
	}
	return refineAffineTransform(field, seeds)
}

func refineAffineTransform(field *edgeField, seeds []scanTransform) scanTransform {
	best := seeds[0]
	bestScore := transformPatternScore(field, best)
	for _, seed := range seeds[1:] {
		score := transformPatternScore(field, seed)
		if score > bestScore {
			best = seed
			bestScore = score
		}
	}

	type paramState struct {
		cx     float64
		cy     float64
		angle  float64
		scaleX float64
		scaleY float64
	}

	state := paramState{
		cx:     best.center.x,
		cy:     best.center.y,
		angle:  transformAngleDeg(best),
		scaleX: best.scaleX,
		scaleY: best.scaleY,
	}

	evaluate := func(p paramState) float64 {
		if p.scaleX <= 0 || p.scaleY <= 0 {
			return -math.MaxFloat64
		}
		return transformPatternScore(field, transformFromParams(p.cx, p.cy, p.angle, p.scaleX, p.scaleY))
	}

	for _, centerStep := range []float64{18, 8, 3, 1} {
		angleStep := 8.0
		scaleStep := maxFloat(state.scaleX, state.scaleY) * 0.10
		improved := true
		for improved {
			improved = false
			currentScore := evaluate(state)
			candidates := []paramState{
				{cx: state.cx - centerStep, cy: state.cy, angle: state.angle, scaleX: state.scaleX, scaleY: state.scaleY},
				{cx: state.cx + centerStep, cy: state.cy, angle: state.angle, scaleX: state.scaleX, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy - centerStep, angle: state.angle, scaleX: state.scaleX, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy + centerStep, angle: state.angle, scaleX: state.scaleX, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy, angle: state.angle - angleStep, scaleX: state.scaleX, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy, angle: state.angle + angleStep, scaleX: state.scaleX, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy, angle: state.angle, scaleX: state.scaleX - scaleStep, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy, angle: state.angle, scaleX: state.scaleX + scaleStep, scaleY: state.scaleY},
				{cx: state.cx, cy: state.cy, angle: state.angle, scaleX: state.scaleX, scaleY: state.scaleY - scaleStep},
				{cx: state.cx, cy: state.cy, angle: state.angle, scaleX: state.scaleX, scaleY: state.scaleY + scaleStep},
			}
			for _, candidate := range candidates {
				score := evaluate(candidate)
				if score > currentScore {
					state = candidate
					currentScore = score
					improved = true
				}
			}
			angleStep *= 0.7
			scaleStep *= 0.7
		}
	}

	return transformFromParams(state.cx, state.cy, state.angle, state.scaleX, state.scaleY)
}

func transformPatternScore(field *edgeField, xform scanTransform) float64 {
	edgeRadii := expectedEdgeRadii(1)
	quietRadii := quietZoneRadii(1)
	edgeEnergy := 0.0
	quietEnergy := 0.0
	samples := 96.0

	for i := 0.0; i < samples; i++ {
		angle := (i / samples) * 2 * math.Pi
		for _, r := range edgeRadii {
			p := transformPoint(xform, r, angle)
			edgeEnergy += field.sampleEdge(p.x, p.y)
		}
		for _, r := range quietRadii {
			p := transformPoint(xform, r, angle)
			quietEnergy += field.sampleEdge(p.x, p.y)
		}
	}

	edgeEnergy /= float64(len(edgeRadii)) * samples
	quietEnergy /= float64(len(quietRadii)) * samples
	return edgeEnergy - quietEnergy*0.7
}

type decodeHit struct {
	url   string
	score float64
}

func decodeRasterCandidate(bitmap *rasterBitmap, field *edgeField, cand rasterCandidate) []decodeHit {
	var hits []decodeHit
	base := estimateAffineTransform(field, cand)
	avgScale := (base.scaleX + base.scaleY) * 0.5
	variants := []scanTransform{
		base,
		{
			center: base.center,
			axisX:  vec2{x: 1},
			axisY:  vec2{y: 1},
			scaleX: avgScale,
			scaleY: avgScale,
		},
		{
			center: vec2{x: cand.cx, y: cand.cy},
			axisX:  vec2{x: 1},
			axisY:  vec2{y: 1},
			scaleX: cand.scale,
			scaleY: cand.scale,
		},
	}

	for _, xform := range variants {
		rotations := findRotationCandidates(bitmap, xform)
		for _, theta := range rotations {
			samples, err := samplePositionSet(bitmap, xform, theta)
			if err != nil {
				continue
			}

			for _, attempt := range buildDecodeAttempts(samples) {
				url, err := decodeBitstream(attempt.bits)
				if err == nil {
					hits = append(hits, decodeHit{url: url, score: attempt.score})
				}
			}
		}
	}
	return hits
}

func findRotationCandidates(bitmap *rasterBitmap, xform scanTransform) []float64 {
	type scoredRotation struct {
		theta float64
		score float64
	}

	var coarse []scoredRotation
	for theta := 0.0; theta < 360.0; theta += searchRotationStepDeg {
		score, ok := rotationScore(bitmap, xform, theta)
		if !ok {
			continue
		}
		coarse = append(coarse, scoredRotation{theta: theta, score: score})
	}

	sort.Slice(coarse, func(i, j int) bool {
		return coarse[i].score > coarse[j].score
	})

	var refined []scoredRotation
	seen := make(map[int]bool)
	for _, cand := range coarse {
		base := int(math.Round(cand.theta))
		if seen[base] {
			continue
		}
		seen[base] = true
		for theta := cand.theta - 0.5; theta <= cand.theta+0.5; theta += refinedRotationStepDeg {
			score, ok := rotationScore(bitmap, xform, theta)
			if !ok {
				continue
			}
			refined = append(refined, scoredRotation{theta: normalizeDegrees(theta), score: score})
		}
		if len(seen) == maxRotationCandidates {
			break
		}
	}

	sort.Slice(refined, func(i, j int) bool {
		return refined[i].score > refined[j].score
	})

	var result []float64
	for _, cand := range refined {
		duplicate := false
		for _, existing := range result {
			if math.Abs(angleDistanceDegrees(existing, cand.theta)) < 0.15 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		result = append(result, cand.theta)
		if len(result) == maxRotationCandidates {
			break
		}
	}
	return result
}

func rotationScore(bitmap *rasterBitmap, xform scanTransform, theta float64) (float64, bool) {
	samples, err := samplePositionSet(bitmap, xform, theta)
	if err != nil {
		return 0, false
	}
	contrasts := make([]float64, len(samples))
	for i, sample := range samples {
		contrasts[i] = sample.contrast
	}
	_, score := otsuThreshold(contrasts)
	return score, true
}

type positionSample struct {
	color      rgbColor
	contrast   float64
	ring       int
	position   int
	bitAngle   float64
	gapAngle   float64
	startAngle float64
}

func samplePositionSet(bitmap *rasterBitmap, xform scanTransform, theta float64) ([]positionSample, error) {
	samples := make([]positionSample, 0, 128)
	for ringIndex := 0; ringIndex < len(ringBitCounts); ringIndex++ {
		bitAngle := 360.0 / float64(ringBitCounts[ringIndex])
		bitAngleRad := bitAngle * deg2rad
		gapAngleRad := ringGapAngles[ringIndex] * deg2rad
		boundaryJitter := gapAngleRad * 0.35
		for pos := 0; pos < ringBitCounts[ringIndex]; pos++ {
			angleDeg := ringRotations[ringIndex] + theta + float64(pos)*bitAngle
			angle := angleDeg * deg2rad

			boundary, ok := sampleRingPatch(bitmap, xform, ringRadii[ringIndex], angle, boundaryJitter, []float64{
				-strokeWidth * 0.22,
				0,
				strokeWidth * 0.22,
			})
			if !ok {
				return nil, fmt.Errorf("sample boundary")
			}

			bgOffset := strokeWidth * 0.85
			innerBG, ok := sampleRingPatch(bitmap, xform, ringRadii[ringIndex]-bgOffset, angle, boundaryJitter*0.6, []float64{0})
			if !ok {
				return nil, fmt.Errorf("sample inner background")
			}
			outerBG, ok := sampleRingPatch(bitmap, xform, ringRadii[ringIndex]+bgOffset, angle, boundaryJitter*0.6, []float64{0})
			if !ok {
				return nil, fmt.Errorf("sample outer background")
			}
			localBG := innerBG.add(outerBG).scale(0.5)

			startOffset := gapAngleRad + 0.35*(bitAngleRad-2*gapAngleRad)
			arcColor, ok := sampleRingPatch(bitmap, xform, ringRadii[ringIndex], angle+startOffset, boundaryJitter, []float64{
				-strokeWidth * 0.18,
				0,
				strokeWidth * 0.18,
			})
			if !ok {
				return nil, fmt.Errorf("sample arc color")
			}

			samples = append(samples, positionSample{
				color:      arcColor,
				contrast:   boundary.distance2(localBG),
				ring:       ringIndex,
				position:   pos,
				bitAngle:   bitAngleRad,
				gapAngle:   gapAngleRad,
				startAngle: angle,
			})
		}
	}
	return samples, nil
}

func sampleRingPatch(bitmap *rasterBitmap, xform scanTransform, radius, angle, angleJitter float64, radialOffsets []float64) (rgbColor, bool) {
	var sum rgbColor
	count := 0.0
	for _, radialOffset := range radialOffsets {
		for _, da := range []float64{-angleJitter, 0, angleJitter} {
			p := transformPoint(xform, radius+radialOffset, angle+da)
			c, ok := bitmap.sampleRGB(p.x, p.y)
			if !ok {
				return rgbColor{}, false
			}
			sum = sum.add(c)
			count++
		}
	}
	return sum.scale(1 / count), true
}

func transformPoint(xform scanTransform, radius, angle float64) vec2 {
	cosA := math.Cos(angle)
	sinA := math.Sin(angle)
	return xform.center.
		add(xform.axisX.scale(xform.scaleX * radius * cosA)).
		add(xform.axisY.scale(xform.scaleY * radius * sinA))
}

type bitAttempt struct {
	bits    []bool
	penalty float64
	score   float64
}

func buildDecodeAttempts(samples []positionSample) []bitAttempt {
	contrasts := make([]float64, len(samples))
	for i, sample := range samples {
		contrasts[i] = sample.contrast
	}

	_, separability := otsuThreshold(contrasts)
	thresholds := thresholdCandidates(contrasts)
	var attempts []bitAttempt
	for _, threshold := range thresholds {
		visibleMask := make([]bool, len(samples))
		visibleCount := 0
		visibleColors := make([]rgbColor, 0, len(samples))
		for i, contrast := range contrasts {
			if contrast < threshold {
				visibleMask[i] = true
				visibleCount++
				visibleColors = append(visibleColors, samples[i].color)
			}
		}
		if visibleCount < minVisiblePositions || visibleCount > len(samples)-8 {
			continue
		}

		assignments := classifyTwoColors(visibleColors)
		for _, mapping := range assignments {
			bits := make([]bool, 0, 128+1+visibleCount)
			assignIndex := 0
			for _, visible := range visibleMask {
				if !visible {
					bits = append(bits, true)
					continue
				}
				bits = append(bits, false)
			}
			bits = append(bits, false)
			for _, visible := range visibleMask {
				if !visible {
					continue
				}
				bits = append(bits, mapping[assignIndex])
				assignIndex++
			}

			penalty := float64(templateMismatch(bits))*10 + math.Abs(float64(visibleCount)-64)
			attempts = append(attempts, bitAttempt{
				bits:    bits,
				penalty: penalty,
				score:   separability*100 - penalty,
			})
		}
	}

	sort.Slice(attempts, func(i, j int) bool {
		return attempts[i].penalty < attempts[j].penalty
	})
	return attempts
}

func thresholdCandidates(contrasts []float64) []float64 {
	otsu, _ := otsuThreshold(contrasts)
	sorted := append([]float64(nil), contrasts...)
	sort.Float64s(sorted)

	var thresholds []float64
	add := func(v float64) {
		for _, existing := range thresholds {
			if math.Abs(existing-v) < 1e-9 {
				return
			}
		}
		thresholds = append(thresholds, v)
	}

	add(otsu)
	for _, visibleCount := range []int{56, 60, 64, 68, 72, 76, 80, 84, 88, 92} {
		if visibleCount <= 0 || visibleCount >= len(sorted) {
			continue
		}
		add((sorted[visibleCount-1] + sorted[visibleCount]) * 0.5)
	}
	return thresholds
}

func classifyTwoColors(colors []rgbColor) [][]bool {
	if len(colors) == 0 {
		return nil
	}

	centerA := colors[0]
	centerB := colors[0]
	maxDist := -1.0
	for _, color := range colors[1:] {
		d := color.distance2(centerA)
		if d > maxDist {
			maxDist = d
			centerB = color
		}
	}

	labels := make([]bool, len(colors))
	for iter := 0; iter < 12; iter++ {
		var sumA, sumB rgbColor
		countA := 0.0
		countB := 0.0

		for i, color := range colors {
			if color.distance2(centerA) <= color.distance2(centerB) {
				labels[i] = false
				sumA = sumA.add(color)
				countA++
			} else {
				labels[i] = true
				sumB = sumB.add(color)
				countB++
			}
		}

		if countA > 0 {
			centerA = sumA.scale(1 / countA)
		}
		if countB > 0 {
			centerB = sumB.scale(1 / countB)
		}
	}

	assignmentA := append([]bool(nil), labels...)
	assignmentB := make([]bool, len(labels))
	for i, label := range labels {
		assignmentB[i] = !label
	}

	if maxDist <= 1e-8 {
		return [][]bool{assignmentA}
	}
	return [][]bool{assignmentA, assignmentB}
}

func templateMismatch(bits []bool) int {
	if len(bits) < 128 {
		return len(templateBits)
	}

	prePerm := make([]bool, 128)
	for i := 0; i < 128; i++ {
		prePerm[i] = bits[gapsBitsOrder[i]]
	}

	mismatch := 0
	for i := 0; i < len(templateBits); i++ {
		if prePerm[120+i] != templateBits[i] {
			mismatch++
		}
	}
	return mismatch
}

func decodeBitstream(bits []bool) (string, error) {
	payload, err := codec.DecodePayload(bits)
	if err != nil {
		return "", err
	}
	return codec.DecompressURL(payload)
}

func otsuThreshold(values []float64) (float64, float64) {
	if len(values) < 2 {
		if len(values) == 1 {
			return values[0], 0
		}
		return 0, 0
	}

	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	prefix := make([]float64, len(sorted)+1)
	prefixSq := make([]float64, len(sorted)+1)
	for i, v := range sorted {
		prefix[i+1] = prefix[i] + v
		prefixSq[i+1] = prefixSq[i] + v*v
	}

	totalMean := prefix[len(sorted)] / float64(len(sorted))
	bestThreshold := sorted[len(sorted)/2]
	bestScore := -1.0

	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			continue
		}
		w0 := float64(i) / float64(len(sorted))
		w1 := 1 - w0
		m0 := prefix[i] / float64(i)
		m1 := (prefix[len(sorted)] - prefix[i]) / float64(len(sorted)-i)
		between := w0 * w1 * (m0 - m1) * (m0 - m1)
		score := between / (totalMean*totalMean + 1e-12)
		if score > bestScore {
			bestScore = score
			bestThreshold = (sorted[i-1] + sorted[i]) * 0.5
		}
	}

	return bestThreshold, bestScore
}

func normalizeDegrees(v float64) float64 {
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	return v
}

func angleDistanceDegrees(a, b float64) float64 {
	d := math.Abs(normalizeDegrees(a) - normalizeDegrees(b))
	if d > 180 {
		d = 360 - d
	}
	return d
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

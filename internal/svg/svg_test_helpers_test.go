package svg

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type ringArc struct {
	Color    int
	StartPos int
	Span     int
}

func extractRingArcsFromFile(t testing.TB, path string) [5][]ringArc {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return extractRingArcsFromBytes(t, data)
}

func extractRingArcsFromBytes(t testing.TB, data []byte) [5][]ringArc {
	t.Helper()
	return parseRingArcsSVG(string(data))
}

func parseRingArcsSVG(svg string) [5][]ringArc {
	cx, cy := 400.0, 400.0
	radii := [5]float64{177.2016, 224.1012, 271.0008, 317.9004, 364.8}
	bits := [5]int{17, 23, 26, 29, 33}
	hgaps := [5]float64{7.5, 5.6, 5.0, 4.2, 3.5}

	re := regexp.MustCompile(`d="M\s+([\d.]+)\s+([\d.]+)\s+A\s+[\d.]+\s+[\d.]+\s+0\s+\d\s+0\s+([\d.]+)\s+([\d.]+)"[^>]*data-color="(\d)"`)

	var result [5][]ringArc
	for ri := 0; ri < 5; ri++ {
		r := radii[ri]
		n := bits[ri]
		ba := 360.0 / float64(n)
		hg := hgaps[ri]

		type pt struct{ x, y float64 }
		starts := make([]pt, n)
		ends := make([]pt, n)
		for i := 0; i < n; i++ {
			a0 := (float64(i)*ba + hg) * math.Pi / 180
			a1 := (float64(i+1)*ba - hg) * math.Pi / 180
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

		ringTag := "name=\"ring-" + strconv.Itoa(ri+1) + "\""
		idx := strings.Index(svg, ringTag)
		if idx < 0 {
			continue
		}
		end := strings.Index(svg[idx:], "</g>") + idx
		chunk := svg[idx:end]

		for _, m := range re.FindAllStringSubmatch(chunk, -1) {
			mx, _ := strconv.ParseFloat(m[1], 64)
			my, _ := strconv.ParseFloat(m[2], 64)
			ax, _ := strconv.ParseFloat(m[3], 64)
			ay, _ := strconv.ParseFloat(m[4], 64)
			color, _ := strconv.Atoi(m[5])

			endP := snap(mx, my, ends)
			startP := snap(ax, ay, starts)
			span := endP - startP + 1
			if span <= 0 {
				span += n
			}
			result[ri] = append(result[ri], ringArc{Color: color, StartPos: startP, Span: span})
		}
	}
	return result
}

func compareRingArcs(apple, ours [5][]ringArc) string {
	for ring := 0; ring < len(apple); ring++ {
		a := apple[ring]
		o := ours[ring]
		if len(a) != len(o) {
			return "ring" + strconv.Itoa(ring+1) + ": apple " + strconv.Itoa(len(a)) + " arcs, ours " + strconv.Itoa(len(o))
		}
		for i := range a {
			if a[i] != o[i] {
				return "ring" + strconv.Itoa(ring+1) + " arc" + strconv.Itoa(i) + ": apple=" + fmt.Sprint(a[i]) + " ours=" + fmt.Sprint(o[i])
			}
		}
	}
	return ""
}

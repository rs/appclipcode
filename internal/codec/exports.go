package codec

var GapsBitsOrderLUT = kGapsBitsOrderLUT

func TLDList() []string {
	return append([]string(nil), tldList...)
}

func FixedTLDIndex() map[string]int {
	out := make(map[string]int, len(fixedTLDIndex))
	for k, v := range fixedTLDIndex {
		out[k] = v
	}
	return out
}

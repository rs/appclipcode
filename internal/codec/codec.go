package codec

// codecVersion represents a codec format version.
type codecVersion int

const (
	version0 codecVersion = 0
	version1 codecVersion = 1
)

// formatParams holds the RS encoding parameters for a codec version.
type formatParams struct {
	gapsDataCount   int
	gapsParityCount int
	arcsDataCount   int
	arcsParityCount int
}

var formats = map[codecVersion]formatParams{
	version0: {gapsDataCount: 9, gapsParityCount: 4, arcsDataCount: 5, arcsParityCount: 2},
	version1: {gapsDataCount: 11, gapsParityCount: 2, arcsDataCount: 5, arcsParityCount: 2},
}

// kGapsBitsOrderLUT is the bit permutation table extracted from the binary.
var kGapsBitsOrderLUT = [128]int{
	16, 0, 1, 2, 4, 5, 6, 7, 30, 31, 32, 33, 34, 36, 37, 38,
	127, 95, 94, 66, 65, 17, 18, 19, 101, 102, 103, 71, 72, 46, 23, 24,
	114, 115, 116, 117, 83, 84, 56, 57, 118, 119, 120, 85, 86, 87, 58, 59,
	96, 97, 98, 67, 68, 41, 42, 20, 104, 105, 73, 74, 75, 47, 48, 25,
	8, 9, 10, 11, 12, 13, 14, 15, 121, 122, 123, 88, 89, 60, 61, 62,
	124, 125, 126, 91, 92, 93, 64, 39, 100, 69, 70, 43, 44, 21, 22, 3,
	111, 112, 113, 80, 81, 82, 54, 53, 106, 107, 108, 76, 77, 49, 50, 26,
	109, 110, 78, 79, 51, 52, 28, 29, 27, 35, 40, 45, 55, 63, 90, 99,
}

// EncodePayload takes compressed URL bytes (up to 16) and produces the final bit vector.
// The output structure is:
//
//	[128 LUT-permuted bits: meta(16)+gaps(104)+template(8)]
//	[1 separator bit (always 0)]
//	[56 arc bits]
//	[extra gap bits: max(0, zeroCount128 - 56) bits from gap vector start]
//
// Total length = 129 + zeroCount128, where zeroCount128 is the number of
// zero bits in the 128-bit pre-permutation vector.
func EncodePayload(payload []byte) ([]bool, error) {
	// Step 1: FitOptimalVersion — strip leading zeros, pick version.
	trimmed := payload
	for len(trimmed) > 0 && trimmed[0] == 0 {
		trimmed = trimmed[1:]
	}
	ver := version0
	if len(trimmed) > 14 {
		ver = version1
	}

	fp := formats[ver]
	totalData := fp.gapsDataCount + fp.arcsDataCount

	// Pad back to totalData bytes with leading zeros.
	padded := make([]byte, totalData)
	if len(trimmed) <= totalData {
		copy(padded[totalData-len(trimmed):], trimmed)
	} else {
		copy(padded, trimmed[len(trimmed)-totalData:])
	}

	// Step 2: scramble — reverse and XOR with 0xa5.
	scrambled := make([]byte, totalData)
	for i := 0; i < totalData; i++ {
		scrambled[i] = padded[totalData-1-i] ^ 0xa5
	}

	// Step 3: split into gaps and arcs data.
	gapsData := scrambled[:fp.gapsDataCount]
	arcsData := scrambled[totalData-fp.arcsDataCount:]

	// Step 4: RS encode gaps (GF(256), fcr=1).
	gapsRS := NewRSEncoder(gf256, fp.gapsParityCount)
	gapsSymbols := make([]int, fp.gapsDataCount)
	for i, b := range gapsData {
		gapsSymbols[i] = int(b)
	}
	gapsEncoded := gapsRS.Encode(gapsSymbols)
	gapsBits := blocksToBits(gapsEncoded, 8) // 104 bits

	// Step 5: gap inversion — invert when zeroCount <= 51.
	gapZeros := 0
	for _, b := range gapsBits {
		if !b {
			gapZeros++
		}
	}
	inverted := false
	if gapZeros <= 51 {
		inverted = true
		for i := range gapsBits {
			gapsBits[i] = !gapsBits[i]
		}
	}

	// Step 6: metadata RS encode (GF(16), fcr=0).
	metaRS := NewRSEncoder(gf16, 2)
	metaData := []int{
		int(ver) >> 3,
		boolToInt(inverted) | ((int(ver) & 7) << 1),
	}
	metaEncoded := metaRS.Encode(metaData)
	metaBits := blocksToBits(metaEncoded, 4) // 16 bits

	// Step 7: arcs RS encode (GF(256), fcr=1).
	arcsRS := NewRSEncoder(gf256, fp.arcsParityCount)
	arcsSymbols := make([]int, fp.arcsDataCount)
	for i, b := range arcsData {
		arcsSymbols[i] = int(b)
	}
	arcsEncoded := arcsRS.Encode(arcsSymbols)
	arcsBits := blocksToBits(arcsEncoded, 8) // 56 bits

	// Step 8: assemble 128 pre-permutation bits.
	// Order: [metadata 16][gaps 104][template 8]
	templateBits := [8]bool{false, true, false, true, false, true, false, false} // 0x2a LSB-first

	prePerm := make([]bool, 128)
	copy(prePerm[0:], metaBits)
	copy(prePerm[16:], gapsBits)
	for i := 0; i < 8; i++ {
		prePerm[120+i] = templateBits[i]
	}

	// Count zeros in the 128-bit assembled vector.
	zeroCount128 := 0
	for _, b := range prePerm {
		if !b {
			zeroCount128++
		}
	}

	// Step 9: LUT permutation — output[LUT[i]] = prePerm[i] for first 128 bits.
	totalLen := 129 + zeroCount128 // 128 + 1 separator + arcs + extra gap bits
	output := make([]bool, totalLen)

	// First copy prePerm to output (will be overwritten by LUT for positions 0-127).
	copy(output, prePerm)
	// Apply LUT permutation.
	for i := 0; i < 128; i++ {
		output[kGapsBitsOrderLUT[i]] = prePerm[i]
	}

	// Step 10: append separator (0), arcs, and extra gap bits.
	pos := 128
	output[pos] = false // separator bit (always 0)
	pos++

	// Arcs bits (56).
	copy(output[pos:], arcsBits)
	pos += len(arcsBits)

	// Extra gap bits: max(0, zeroCount128 - len(arcsBits)) bits from gap vector start.
	extraCount := zeroCount128 - len(arcsBits)
	if extraCount > 0 && extraCount <= len(gapsBits) {
		copy(output[pos:], gapsBits[:extraCount])
	}

	return output, nil
}

// blocksToBits converts int symbols to a bit vector, MSB first per symbol.
func blocksToBits(symbols []int, bitsPerSymbol int) []bool {
	bits := make([]bool, len(symbols)*bitsPerSymbol)
	for i, sym := range symbols {
		for j := bitsPerSymbol - 1; j >= 0; j-- {
			bits[i*bitsPerSymbol+(bitsPerSymbol-1-j)] = (sym>>j)&1 == 1
		}
	}
	return bits
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

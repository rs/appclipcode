package codec

// RSEncoder encodes data with Reed-Solomon error correction.
type RSEncoder struct {
	gf        *GaloisField
	genPoly   []int // generator polynomial, genPoly[0] = leading (highest degree) coefficient = 1
	numParity int
}

// NewRSEncoder creates an RS encoder over the given field with numParity parity symbols.
func NewRSEncoder(gf *GaloisField, numParity int) *RSEncoder {
	enc := &RSEncoder{
		gf:        gf,
		numParity: numParity,
	}
	enc.buildGenerator(numParity)
	return enc
}

func (enc *RSEncoder) buildGenerator(numParity int) {
	// g(x) = (x + α^fcr)(x + α^(fcr+1))...(x + α^(fcr+numParity-1))
	// Stored highest-degree-first: genPoly[0] = coeff of x^numParity (always 1)
	gen := []int{1} // start with constant polynomial 1

	for i := 0; i < numParity; i++ {
		root := enc.gf.Exp(enc.gf.fcrBase + i)
		// Multiply gen by (x + root)
		newGen := make([]int, len(gen)+1)
		// x * gen: shift coefficients right by 1
		copy(newGen, gen)
		// + root * gen: add root * each coefficient
		for j := 0; j < len(gen); j++ {
			newGen[j+1] ^= enc.gf.Multiply(gen[j], root)
		}
		gen = newGen
	}

	enc.genPoly = gen
}

// Encode returns a codeword: [data symbols..., parity symbols...].
// Uses systematic encoding: data is preserved, parity is appended.
func (enc *RSEncoder) Encode(data []int) []int {
	n := len(data) + enc.numParity
	result := make([]int, n)
	copy(result, data)

	// Polynomial long division of data*x^numParity by genPoly
	for i := 0; i < len(data); i++ {
		coef := result[i]
		if coef != 0 {
			for j := 1; j <= enc.numParity; j++ {
				result[i+j] ^= enc.gf.Multiply(enc.genPoly[j], coef)
			}
		}
	}

	// Restore data (division zeroed it out), keep parity
	copy(result, data)
	return result
}

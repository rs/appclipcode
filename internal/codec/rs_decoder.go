package codec

import "fmt"

func decodeRSCodeword(gf *GaloisField, codeword []int, numParity int) ([]int, error) {
	if numParity == 0 {
		out := make([]int, len(codeword))
		copy(out, codeword)
		return out, nil
	}

	syndromes := rsSyndromes(gf, codeword, numParity)
	if rsSyndromesZero(syndromes) {
		out := make([]int, len(codeword))
		copy(out, codeword)
		return out, nil
	}

	if corrected, ok := correctSingleRSError(gf, codeword, syndromes); ok {
		return corrected, nil
	}
	if numParity >= 4 {
		if corrected, ok := correctDoubleRSError(gf, codeword, syndromes); ok {
			return corrected, nil
		}
	}

	return nil, fmt.Errorf("reed-solomon decode failed")
}

func rsSyndromes(gf *GaloisField, codeword []int, numParity int) []int {
	syndromes := make([]int, numParity)
	for si := 0; si < numParity; si++ {
		root := gfPow(gf, gf.fcrBase+si)
		acc := 0
		for _, sym := range codeword {
			acc = gf.Multiply(acc, root) ^ sym
		}
		syndromes[si] = acc
	}
	return syndromes
}

func rsSyndromesZero(syndromes []int) bool {
	for _, s := range syndromes {
		if s != 0 {
			return false
		}
	}
	return true
}

func correctSingleRSError(gf *GaloisField, codeword, syndromes []int) ([]int, bool) {
	n := len(codeword)
	for pos := 0; pos < n; pos++ {
		a0 := rsErrorTerm(gf, n, pos, 0)
		if a0 == 0 {
			continue
		}
		errMag := gf.Multiply(syndromes[0], gf.Inverse(a0))
		if errMag == 0 {
			continue
		}

		ok := true
		for si, syndrome := range syndromes {
			if gf.Multiply(errMag, rsErrorTerm(gf, n, pos, si)) != syndrome {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}

		out := append([]int(nil), codeword...)
		out[pos] ^= errMag
		if rsSyndromesZero(rsSyndromes(gf, out, len(syndromes))) {
			return out, true
		}
	}
	return nil, false
}

func correctDoubleRSError(gf *GaloisField, codeword, syndromes []int) ([]int, bool) {
	n := len(codeword)
	if len(syndromes) < 2 {
		return nil, false
	}

	for p := 0; p < n; p++ {
		for q := p + 1; q < n; q++ {
			a0 := rsErrorTerm(gf, n, p, 0)
			b0 := rsErrorTerm(gf, n, q, 0)
			a1 := rsErrorTerm(gf, n, p, 1)
			b1 := rsErrorTerm(gf, n, q, 1)

			det := gf.Multiply(a0, b1) ^ gf.Multiply(a1, b0)
			if det == 0 {
				continue
			}

			errP := gf.Multiply(gf.Multiply(syndromes[0], b1)^gf.Multiply(syndromes[1], b0), gf.Inverse(det))
			errQ := gf.Multiply(gf.Multiply(a0, syndromes[1])^gf.Multiply(a1, syndromes[0]), gf.Inverse(det))
			if errP == 0 || errQ == 0 {
				continue
			}

			ok := true
			for si, syndrome := range syndromes {
				reconstructed := gf.Multiply(errP, rsErrorTerm(gf, n, p, si)) ^ gf.Multiply(errQ, rsErrorTerm(gf, n, q, si))
				if reconstructed != syndrome {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}

			out := append([]int(nil), codeword...)
			out[p] ^= errP
			out[q] ^= errQ
			if rsSyndromesZero(rsSyndromes(gf, out, len(syndromes))) {
				return out, true
			}
		}
	}

	return nil, false
}

func rsErrorTerm(gf *GaloisField, codewordLen, pos, syndromeIndex int) int {
	exponent := (gf.fcrBase + syndromeIndex) * (codewordLen - 1 - pos)
	return gfPow(gf, exponent)
}

func gfPow(gf *GaloisField, exponent int) int {
	order := gf.size - 1
	exponent %= order
	if exponent < 0 {
		exponent += order
	}
	return gf.Exp(exponent)
}

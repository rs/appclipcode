package codec

// GaloisField implements arithmetic over GF(2^n).
type GaloisField struct {
	size    int   // number of elements (16 or 256)
	genPoly int   // primitive polynomial
	fcrBase int   // first consecutive root
	expTbl  []int // antilog table
	logTbl  []int // log table
}

// NewGF creates a Galois field with the given primitive polynomial, size, and generator base.
func NewGF(primitive, size, genBase int) *GaloisField {
	gf := &GaloisField{
		size:    size,
		genPoly: primitive,
		fcrBase: genBase,
		expTbl:  make([]int, size*2),
		logTbl:  make([]int, size),
	}

	x := 1
	for i := 0; i < size; i++ {
		gf.expTbl[i] = x
		gf.logTbl[x] = i
		x <<= 1
		if x >= size {
			x ^= primitive
			x &= size - 1
		}
	}
	for i := size; i < size*2; i++ {
		gf.expTbl[i] = gf.expTbl[i-size+1]
	}

	return gf
}

// Exp returns α^a (antilog).
func (gf *GaloisField) Exp(a int) int { return gf.expTbl[a] }

// Log returns log_α(a).
func (gf *GaloisField) Log(a int) int { return gf.logTbl[a] }

// Multiply returns a × b in the field.
func (gf *GaloisField) Multiply(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return gf.expTbl[gf.logTbl[a]+gf.logTbl[b]]
}

// Inverse returns a^(-1) in the field.
func (gf *GaloisField) Inverse(a int) int {
	return gf.expTbl[gf.size-1-gf.logTbl[a]]
}

// Pre-built fields used by the codec.
var (
	gf16  = NewGF(0x13, 16, 0)   // GF(2^4), x^4+x+1, fcr=0
	gf256 = NewGF(0x11D, 256, 1) // GF(2^8), x^8+x^4+x^3+x^2+1, fcr=1
)

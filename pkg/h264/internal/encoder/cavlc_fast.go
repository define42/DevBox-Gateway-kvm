package encoder

const cavlcLevelLimit = 64

type cavlcLevelSymbol struct {
	code       uint32
	length     uint8
	nextSuffix uint8
}

// Each table entry holds the code in bits 0..15, length in bits 16..23, and
// next suffix in bits 24..31. Packing keeps all fields in one word per symbol.
// The 8 KiB table includes a padding suffix row so the writer can prove its
// internally generated suffix index is in bounds. Larger coefficients retain
// the original escape/overflow path.
var cavlcLevelTable = makeCavlcLevelTable()

func makeCavlcLevelTable() [2][8][2 * cavlcLevelLimit]uint32 {
	var table [2][8][2 * cavlcLevelLimit]uint32
	for adjustment := range table {
		for suffix := 0; suffix < 7; suffix++ {
			for index := range table[adjustment][suffix] {
				value := int32(index - cavlcLevelLimit)
				// Zero is never a level; adjusted ±1 are trailing ones and
				// cannot be the first non-trailing level with this adjustment.
				if value == 0 || (adjustment != 0 && (value == 1 || value == -1)) {
					continue
				}
				symbol, _ := cavlcLevelSymbolSlow(value, int32(suffix), adjustment != 0)
				table[adjustment][suffix][index] = packCavlcLevelSymbol(symbol)
			}
		}
	}
	return table
}

// Successful symbols for signed int16 levels and suffixes 0..6 have at most
// 13 code bits, 28 total bits, and a next suffix no larger than 6. Keep the
// escape calculation separate so its overflow exit remains unchanged.
func packCavlcLevelSymbol(symbol cavlcLevelSymbol) uint32 {
	return symbol.code | uint32(symbol.length)<<16 | uint32(symbol.nextSuffix)<<24
}

func cavlcLevelSymbolSlow(value, suffix int32, adjust bool) (cavlcLevelSymbol, bool) {
	code := (value - 1) * 2
	sign := uint32(code >> 31)
	code = int32((uint32(code) ^ sign) + (sign << 1))
	if adjust {
		code -= 2
	}
	prefix := code >> uint(suffix)
	suffixSize := suffix
	suffixValue := code - (prefix << uint(suffix))
	if prefix >= 14 && prefix < 30 && suffix == 0 {
		prefix = 14
		suffixValue = code - prefix
		suffixSize = 4
	} else if prefix >= 15 {
		prefix = 15
		suffixValue = code - (prefix << uint(suffix))
		if suffixValue>>11 != 0 {
			return cavlcLevelSymbol{}, false
		}
		if suffix == 0 {
			suffixValue -= 15
		}
		suffixSize = 12
	}
	length := prefix + 1 + suffixSize
	word := (1 << uint(suffixSize)) | suffixValue
	if suffix == 0 {
		suffix++
	}
	threshold := int32(3) << uint(suffix-1)
	if (value > threshold || value < -threshold) && suffix < 6 {
		suffix++
	}
	return cavlcLevelSymbol{code: uint32(word), length: uint8(length), nextSuffix: uint8(suffix)}, true
}

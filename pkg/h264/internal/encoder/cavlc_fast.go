package encoder

const cavlcLevelLimit = 64

type cavlcLevelSymbol struct {
	code       uint32
	length     uint8
	nextSuffix uint8
}

// About 14 KiB covers common signed levels for every suffix and first-level
// adjustment. Larger coefficients retain the original escape/overflow path.
var cavlcLevelTable = makeCavlcLevelTable()

func makeCavlcLevelTable() [2][7][2 * cavlcLevelLimit]cavlcLevelSymbol {
	var table [2][7][2 * cavlcLevelLimit]cavlcLevelSymbol
	for adjustment := range table {
		for suffix := range table[adjustment] {
			for index := range table[adjustment][suffix] {
				value := int32(index - cavlcLevelLimit)
				// Zero is never a level; adjusted ±1 are trailing ones and
				// cannot be the first non-trailing level with this adjustment.
				if value == 0 || (adjustment != 0 && (value == 1 || value == -1)) {
					continue
				}
				table[adjustment][suffix][index], _ = cavlcLevelSymbolSlow(value, int32(suffix), adjustment != 0)
			}
		}
	}
	return table
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

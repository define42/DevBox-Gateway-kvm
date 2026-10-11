package cloudinit

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

func (image *seedISO) rockRidge(entries []seedEntry) (int, bool, error) {
	for _, entry := range entries {
		if entry.name != "\x00" {
			continue
		}
		data := entry.systemUse
		if len(data) < 7 || string(data[:2]) != "SP" {
			return 0, false, nil
		}
		if data[2] != 7 || data[3] != 1 || data[4] != 0xbe || data[5] != 0xef {
			return 0, false, errInvalidISO
		}
		fields, err := image.systemUseFields(data, 0)
		if err != nil {
			return 0, false, err
		}
		for _, field := range fields {
			if string(field[:2]) != "ER" {
				continue
			}
			if len(field) < 8 || int(field[4])+8 > len(field) {
				return 0, false, errInvalidISO
			}
			switch string(field[8 : 8+int(field[4])]) {
			case "RRIP_1991A", "IEEE_P1282", "IEEE_1282":
				return int(data[6]), true, nil
			}
		}

		return 0, false, nil
	}

	return 0, false, nil
}

func (image *seedISO) entryName(entry seedEntry, skip int, rockRidge, joliet bool) (string, bool, error) {
	regular := entry.flags&(2|128) == 0
	if entry.name == "\x00" || entry.name == "\x01" {
		return "", regular, nil
	}
	name, err := isoEntryName(entry.name, joliet)
	if err != nil || !rockRidge {
		return name, regular, err
	}
	if skip > len(entry.systemUse) {
		return "", false, errInvalidISO
	}
	fields, err := image.systemUseFields(entry.systemUse[skip:], 0)
	if err != nil {
		return "", false, err
	}
	var alternate strings.Builder
	for _, field := range fields {
		switch string(field[:2]) {
		case "NM":
			if len(field) < 5 || field[4]&^byte(1) != 0 || alternate.Len()+len(field)-5 > 255 {
				return "", false, errInvalidISO
			}
			alternate.Write(field[5:])
		case "SL":
			regular = false
		case "PX":
			if len(field) < 36 {
				return "", false, errInvalidISO
			}
			mode, ok := isoUint32(field[4:12])
			if !ok {
				return "", false, errInvalidISO
			}
			regular = regular && mode&0o170000 == 0o100000
		}
	}
	if alternate.Len() > 0 {
		name = alternate.String()
	}

	return name, regular, nil
}

func isoEntryName(name string, joliet bool) (string, error) {
	if joliet {
		if len(name)%2 != 0 {
			return "", errInvalidISO
		}
		units := make([]uint16, len(name)/2)
		for i := range units {
			units[i] = binary.BigEndian.Uint16([]byte(name[2*i : 2*i+2]))
		}
		name = string(utf16.Decode(units))
	} else {
		name = strings.ToLower(name)
	}
	name, _, _ = strings.Cut(name, ";")

	return strings.TrimSuffix(name, "."), nil
}

func (image *seedISO) systemUseFields(data []byte, depth int) ([][]byte, error) {
	if depth >= maxRRDepth {
		return nil, errInvalidISO
	}
	var fields [][]byte
	for len(data) >= 4 {
		length := int(data[2])
		if length < 4 || length > len(data) || data[3] != 1 {
			return nil, errInvalidISO
		}
		field := data[:length]
		switch string(field[:2]) {
		case "ST":
			return fields, nil
		case "CE":
			continuation, err := image.continuation(field)
			if err != nil {
				return nil, err
			}
			more, err := image.systemUseFields(continuation, depth+1)
			if err != nil {
				return nil, err
			}
			fields = append(fields, more...)
		default:
			fields = append(fields, field)
		}
		data = data[length:]
	}

	return fields, nil
}

func (image *seedISO) continuation(field []byte) ([]byte, error) {
	if len(field) != 28 {
		return nil, errInvalidISO
	}
	block, blockOK := isoUint32(field[4:12])
	offset, offsetOK := isoUint32(field[12:20])
	length, lengthOK := isoUint32(field[20:28])
	if !blockOK || !offsetOK || !lengthOK || uint64(length) > uint64(image.rrBudget) {
		return nil, errInvalidISO
	}
	image.rrBudget -= int(length)

	return image.region(uint64(block)*isoSectorSize+uint64(offset), uint64(length))
}

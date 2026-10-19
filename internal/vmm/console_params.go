package vmm

import "strings"

// consoleBootParams directs an RDP guest's kernel output to its framebuffer.
// Serial and early-console arguments would otherwise select the UART even
// when the desktop itself is presented through RDP.
func (v *VMM) consoleBootParams(params string) string {
	if v.RDPListenerFile == nil {
		return params
	}

	var output strings.Builder
	output.WriteString("console=tty0")
	for pos := 0; pos < len(params); {
		spaceStart := pos
		for pos < len(params) && strings.IndexByte(" \t\n\r\v\f\xa0", params[pos]) >= 0 {
			pos++
		}
		start := pos
		inQuote := false
		for pos < len(params) {
			ch := params[pos]
			if strings.IndexByte(" \t\n\r\v\f\xa0", ch) >= 0 && !inQuote {
				break
			}
			if ch == '"' {
				inQuote = !inQuote
			}
			pos++
		}
		if start == pos {
			output.WriteString(params[spaceStart:])

			break
		}

		fields := kernelParamFields(params[start:pos])
		if len(fields) == 0 {
			// The -- delimiter ends kernel options. Keep init's arguments,
			// including their quotes and whitespace, exactly as supplied.
			if start == spaceStart {
				output.WriteByte(' ')
			}
			output.WriteString(params[spaceStart:])

			break
		}
		switch kernelParamName(fields[0]) {
		case "console", "earlyprintk", "earlycon":
			continue
		}

		if start == spaceStart {
			output.WriteByte(' ')
		}
		output.WriteString(params[spaceStart:pos])
	}

	return output.String()
}

package gateway

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readProcessMemory returns resident anonymous proportional-set-size bytes.
// Only anonymous PSS may offset an outstanding guest memory reservation: total
// RSS also includes shared and file-backed pages, which cannot safely reduce it.
// Swapped-out anonymous memory is not credited because it may need RAM again.
func readProcessMemory(pid int) (uint64, error) {
	return readProcessMemoryFrom(pid, os.ReadFile)
}

func readProcessMemoryFrom(pid int, readFile func(string) ([]byte, error)) (uint64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid process ID %d: %w", pid, ErrInvalid)
	}
	data, err := readFile("/proc/" + strconv.Itoa(pid) + "/smaps_rollup")
	if err != nil {
		return 0, fmt.Errorf("read process %d memory: %w", pid, err)
	}

	return parseProcessMemory(string(data))
}

// parseProcessMemory accepts only the kernel's integer-kilobyte Pss_Anon field.
// Missing or unreadable accounting must remain an error so admission can give
// the process no resident-memory credit instead of falling back to total RSS.
func parseProcessMemory(data string) (uint64, error) {
	var resident uint64
	var found bool
	for line := range strings.SplitSeq(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "Pss_Anon:" {
			continue
		}
		if found || len(fields) != 3 || fields[2] != "kB" {
			return 0, fmt.Errorf("invalid anonymous PSS field %q: %w", line, ErrInvalid)
		}
		value, err := parseMemoryNumber(fields[1])
		if err != nil || value > ^uint64(0)/1024 {
			return 0, fmt.Errorf("invalid anonymous PSS value %q: %w", fields[1], ErrInvalid)
		}
		resident, found = value*1024, true
	}
	if !found {
		return 0, fmt.Errorf("missing anonymous PSS: %w", ErrInvalid)
	}

	return resident, nil
}

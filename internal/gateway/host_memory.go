package gateway

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

type hostMemory struct {
	Total, Available uint64
}

type memoryCgroup struct {
	member, root, mount string
	unified             bool
}

func readHostMemory() (hostMemory, error) {
	return readHostMemoryFrom(os.ReadFile)
}

func readHostMemoryFrom(readFile func(string) ([]byte, error)) (hostMemory, error) {
	data, err := readFile("/proc/meminfo")
	if err != nil {
		return hostMemory{}, fmt.Errorf("read host memory: %w", err)
	}
	memory, err := parseHostMeminfo(string(data))
	if err != nil {
		return hostMemory{}, err
	}
	data, err = readFile("/proc/self/cgroup")
	if errors.Is(err, os.ErrNotExist) {
		return memory, nil // The kernel may have no cgroup support.
	}
	if err != nil {
		return hostMemory{}, fmt.Errorf("read cgroup membership: %w", err)
	}
	group, found, err := parseMemoryMembership(string(data))
	if err != nil || !found {
		return memory, err
	}
	data, err = readFile("/proc/self/mountinfo")
	if err != nil {
		return hostMemory{}, fmt.Errorf("read cgroup mounts: %w", err)
	}
	group, err = findMemoryMount(string(data), group)
	if err != nil {
		return hostMemory{}, err
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(group.member, group.root), "/")
	directory := path.Join(group.mount, relative)
	for {
		if err := constrainCgroupMemory(readFile, directory, group, &memory); err != nil {
			return hostMemory{}, err
		}
		if directory == group.mount {
			break
		}
		directory = path.Dir(directory)
	}

	return memory, nil
}

func parseHostMeminfo(data string) (hostMemory, error) {
	values := make(map[string]uint64, 2)
	for line := range strings.SplitSeq(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || (fields[0] != "MemTotal:" && fields[0] != "MemAvailable:") {
			continue
		}
		if len(fields) != 3 || fields[2] != "kB" {
			return hostMemory{}, fmt.Errorf("invalid meminfo field %q: %w", line, ErrInvalid)
		}
		if _, exists := values[fields[0]]; exists {
			return hostMemory{}, fmt.Errorf("duplicate meminfo field %s: %w", fields[0], ErrInvalid)
		}
		value, err := parseMemoryNumber(fields[1])
		if err != nil || value > ^uint64(0)/1024 {
			return hostMemory{}, fmt.Errorf("invalid meminfo value %q: %w", fields[1], ErrInvalid)
		}
		values[fields[0]] = value * 1024
	}
	total, totalOK := values["MemTotal:"]
	available, availableOK := values["MemAvailable:"]
	if !totalOK || !availableOK || total == 0 || available > total {
		return hostMemory{}, fmt.Errorf("missing or inconsistent host memory totals: %w", ErrInvalid)
	}

	return hostMemory{Total: total, Available: available}, nil
}

func parseMemoryNumber(value string) (uint64, error) {
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, fmt.Errorf("invalid memory value %q: %w", value, ErrInvalid)
	}

	return strconv.ParseUint(value, 10, 64)
}

func parseMemoryMembership(data string) (memoryCgroup, bool, error) {
	var unified, legacy *memoryCgroup
	for line := range strings.SplitSeq(data, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			return memoryCgroup{}, false, fmt.Errorf("invalid cgroup membership: %w", ErrInvalid)
		}
		hierarchy, err := parseMemoryNumber(fields[0])
		if err != nil || (hierarchy == 0) != (fields[1] == "") {
			return memoryCgroup{}, false, fmt.Errorf("invalid cgroup hierarchy: %w", ErrInvalid)
		}
		isUnified := hierarchy == 0
		if !isUnified && !hasMemoryToken(fields[1], ",") {
			continue
		}
		// Membership paths are raw; only mountinfo paths use octal escaping.
		if err := validateCgroupPath(fields[2]); err != nil {
			return memoryCgroup{}, false, err
		}
		group := &memoryCgroup{member: fields[2], unified: isUnified}
		if isUnified {
			if unified != nil {
				return memoryCgroup{}, false, fmt.Errorf("duplicate unified cgroup: %w", ErrInvalid)
			}
			unified = group
		} else {
			if legacy != nil {
				return memoryCgroup{}, false, fmt.Errorf("duplicate memory cgroup: %w", ErrInvalid)
			}
			legacy = group
		}
	}
	// A controller can belong to only one hierarchy. Hybrid systems can have
	// a unified membership while the memory controller remains on v1.
	if legacy != nil {
		return *legacy, true, nil
	}
	if unified != nil {
		return *unified, true, nil
	}

	return memoryCgroup{}, false, nil
}

func findMemoryMount(data string, group memoryCgroup) (memoryCgroup, error) {
	for line := range strings.SplitSeq(data, "\n") {
		if line == "" {
			continue
		}
		before, after, ok := strings.Cut(line, " - ")
		if !ok {
			return memoryCgroup{}, fmt.Errorf("invalid mountinfo record: %w", ErrInvalid)
		}
		fields, filesystem := strings.Fields(before), strings.Fields(after)
		if len(filesystem) < 3 || len(fields) < 6 {
			return memoryCgroup{}, fmt.Errorf("invalid mountinfo record: %w", ErrInvalid)
		}
		if (group.unified && filesystem[0] != "cgroup2") ||
			(!group.unified && (filesystem[0] != "cgroup" || !hasMemoryToken(filesystem[2], ","))) {
			continue
		}
		root, err := decodeCgroupPath(fields[3])
		if err != nil {
			return memoryCgroup{}, err
		}
		// An inherited mount can refer to an unresolvable namespace ancestor.
		if root == "/.." || strings.HasPrefix(root, "/../") {
			continue
		}
		if err := validateCgroupPath(root); err != nil {
			return memoryCgroup{}, err
		}
		mount, err := decodeCgroupPath(fields[4])
		if err != nil {
			return memoryCgroup{}, err
		}
		if err := validateCgroupPath(mount); err != nil {
			return memoryCgroup{}, err
		}
		if root != "/" && group.member != root && !strings.HasPrefix(group.member, root+"/") {
			continue
		}
		// Prefer the broadest mount so a leaf bind mount cannot hide a tighter
		// ancestor. Membership and mount roots use cgroup-namespace coordinates.
		if group.mount == "" || len(root) < len(group.root) {
			group.root, group.mount = root, mount
		}
	}
	if group.mount == "" {
		return memoryCgroup{}, fmt.Errorf("cannot locate visible memory cgroup: %w", ErrInvalid)
	}

	return group, nil
}

func decodeCgroupPath(value string) (string, error) {
	var decoded strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			decoded.WriteByte(value[i])

			continue
		}
		if i+3 >= len(value) {
			return "", fmt.Errorf("invalid escaped cgroup path: %w", ErrInvalid)
		}
		var escaped byte
		switch value[i+1 : i+4] {
		case "040":
			escaped = ' '
		case "011":
			escaped = '\t'
		case "012":
			escaped = '\n'
		case "134":
			escaped = '\\'
		default:
			return "", fmt.Errorf("invalid escaped cgroup path: %w", ErrInvalid)
		}
		decoded.WriteByte(escaped)
		i += 3
	}

	return decoded.String(), nil
}

func validateCgroupPath(value string) error {
	if !strings.HasPrefix(value, "/") || path.Clean(value) != value || strings.ContainsRune(value, 0) {
		return fmt.Errorf("unsafe cgroup path %q: %w", value, ErrInvalid)
	}

	return nil
}

func hasMemoryToken(value, separator string) bool {
	for token := range strings.SplitSeq(value, separator) {
		if token == "memory" {
			return true
		}
	}

	return false
}

func constrainCgroupMemory(
	readFile func(string) ([]byte, error), directory string, group memoryCgroup, memory *hostMemory,
) error {
	limitName, usageName := "memory.limit_in_bytes", "memory.usage_in_bytes"
	if group.unified {
		limitName, usageName = "memory.max", "memory.current"
	}
	limitData, limitErr := readFile(path.Join(directory, limitName))
	usageData, usageErr := readFile(path.Join(directory, usageName))
	if group.unified && errors.Is(limitErr, os.ErrNotExist) && errors.Is(usageErr, os.ErrNotExist) {
		absent, err := cgroupMemoryAbsent(readFile, directory, group)
		if err != nil || absent {
			return err
		}
	}
	if limitErr != nil || usageErr != nil {
		return fmt.Errorf("read cgroup memory at %s: %w", directory, errors.Join(limitErr, usageErr))
	}
	usage, err := parseMemoryNumber(strings.TrimSpace(string(usageData)))
	if err != nil {
		return fmt.Errorf("invalid cgroup usage at %s: %w", directory, err)
	}
	limitText := strings.TrimSpace(string(limitData))
	if group.unified && limitText == "max" {
		return nil
	}
	limit, err := parseMemoryNumber(limitText)
	if err != nil {
		return fmt.Errorf("invalid cgroup limit at %s: %w", directory, err)
	}
	memory.Total = min(memory.Total, limit)
	remaining := uint64(0)
	if usage < limit {
		remaining = limit - usage
	}
	memory.Available = min(memory.Available, remaining)

	return nil
}

// cgroupMemoryAbsent distinguishes a disabled memory controller and the actual
// hierarchy root from unreadable or incomplete controls on a limited group.
func cgroupMemoryAbsent(
	readFile func(string) ([]byte, error), directory string, group memoryCgroup,
) (bool, error) {
	controllers, err := readFile(path.Join(directory, "cgroup.controllers"))
	if err != nil {
		return false, fmt.Errorf("read cgroup controllers at %s: %w", directory, err)
	}
	if !hasMemoryToken(strings.Join(strings.Fields(string(controllers)), " "), " ") {
		return true, nil
	}
	if directory != group.mount || group.root != "/" {
		return false, nil
	}
	// cgroup.type is absent only at the actual hierarchy root. A limited
	// namespace root also reports /, but must expose its memory controls.
	_, typeErr := readFile(path.Join(directory, "cgroup.type"))
	if errors.Is(typeErr, os.ErrNotExist) {
		return true, nil
	}
	if typeErr != nil {
		return false, fmt.Errorf("read cgroup type at %s: %w", directory, typeErr)
	}

	return false, nil
}

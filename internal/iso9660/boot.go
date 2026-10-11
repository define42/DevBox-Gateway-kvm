package iso9660

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

type BootFiles struct {
	Kernel     []byte
	Initrd     []byte
	Cmdline    string
	KernelPath string
	InitrdPath string
}

var ErrNoBootKernel = errors.New("iso9660: no bootable Linux kernel found")

type bootSpec struct {
	kernelPath string
	initrdPath string
	cmdline    string
}

type syslinuxStanza struct {
	label      string
	kernelPath string
	initrdPath string
	appendLine string
}

func LoadBootFiles(r *Reader) (*BootFiles, error) {
	spec := r.findBootSpec()
	if spec.kernelPath == "" {
		spec = r.findBootSpecByPath()
	}

	if spec.kernelPath == "" {
		return nil, ErrNoBootKernel
	}

	kernel, err := r.ReadFile(spec.kernelPath)
	if err != nil {
		return nil, fmt.Errorf("kernel %s: %w", spec.kernelPath, err)
	}

	var initrd []byte
	if spec.initrdPath != "" {
		initrd, err = r.ReadFile(spec.initrdPath)
		if err != nil {
			return nil, fmt.Errorf("initrd %s: %w", spec.initrdPath, err)
		}
	}

	return &BootFiles{
		Kernel:     kernel,
		Initrd:     initrd,
		Cmdline:    spec.cmdline,
		KernelPath: spec.kernelPath,
		InitrdPath: spec.initrdPath,
	}, nil
}

func (r *Reader) findBootSpec() bootSpec {
	if spec := r.findBootSpecByElTorito(); spec.kernelPath != "" {
		return spec
	}

	for _, name := range []string{
		"/boot/isolinux/isolinux.cfg",
		"/isolinux/isolinux.cfg",
		"/boot/syslinux/syslinux.cfg",
		"/syslinux.cfg",
	} {
		data, err := r.ReadFile(name)
		if err != nil {
			continue
		}

		if spec := parseSyslinuxConfig(name, string(data)); spec.kernelPath != "" {
			return spec
		}
	}

	for _, name := range []string{
		"/boot/grub/grub.cfg",
		"/grub/grub.cfg",
		"/efi/boot/grub.cfg",
	} {
		data, err := r.ReadFile(name)
		if err != nil {
			continue
		}

		if spec := parseGRUBConfig(name, string(data)); spec.kernelPath != "" {
			return spec
		}
	}

	return bootSpec{}
}

func (r *Reader) findBootSpecByElTorito() bootSpec {
	bootImagePath, err := r.elToritoBootImagePath()
	if err != nil {
		return bootSpec{}
	}

	for _, name := range elToritoSyslinuxConfigCandidates(bootImagePath) {
		data, err := r.ReadFile(name)
		if err != nil {
			continue
		}

		if spec := parseSyslinuxConfig(name, string(data)); spec.kernelPath != "" {
			return spec
		}
	}

	for _, name := range elToritoGRUBConfigCandidates(bootImagePath) {
		data, err := r.ReadFile(name)
		if err != nil {
			continue
		}

		if spec := parseGRUBConfig(name, string(data)); spec.kernelPath != "" {
			return spec
		}
	}

	return bootSpec{}
}

func elToritoSyslinuxConfigCandidates(bootImagePath string) []string {
	dir := path.Dir(bootImagePath)

	return uniquePaths(
		path.Join(dir, "isolinux.cfg"),
		path.Join(dir, "syslinux.cfg"),
	)
}

func elToritoGRUBConfigCandidates(bootImagePath string) []string {
	dir := path.Dir(bootImagePath)
	parent := path.Dir(dir)

	return uniquePaths(
		path.Join(dir, "grub.cfg"),
		path.Join(parent, "grub.cfg"),
	)
}

func uniquePaths(names ...string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(names))

	for _, name := range names {
		name = path.Clean(name)
		if seen[name] {
			continue
		}

		seen[name] = true
		unique = append(unique, name)
	}

	return unique
}

func (r *Reader) findBootSpecByPath() bootSpec {
	kernels := []string{
		"/boot/vmlinuz",
		"/boot/vmlinuz64",
		"/boot/bzimage",
		"/casper/vmlinuz",
		"/live/vmlinuz",
		"/isolinux/vmlinuz",
		"/images/pxeboot/vmlinuz",
		"/bzimage",
	}
	initrds := []string{
		"/boot/core.gz",
		"/boot/initrd.gz",
		"/boot/initrd",
		"/casper/initrd",
		"/casper/initrd.lz",
		"/live/initrd.img",
		"/isolinux/initrd.img",
		"/isolinux/initrd.gz",
		"/images/pxeboot/initrd.img",
	}

	spec := bootSpec{}
	for _, name := range kernels {
		if _, err := r.lookup(name); err == nil {
			spec.kernelPath = name

			break
		}
	}

	for _, name := range initrds {
		if _, err := r.lookup(name); err == nil {
			spec.initrdPath = name

			break
		}
	}

	return spec
}

func parseSyslinuxConfig(configPath, data string) bootSpec {
	var (
		defaultLabel string
		stanzas      []*syslinuxStanza
		cur          = &syslinuxStanza{}
	)

	stanzas = append(stanzas, cur)
	for _, line := range linesWithoutComments(data) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		key := strings.ToLower(fields[0])
		rest := strings.TrimSpace(line[len(fields[0]):])

		switch key {
		case "default":
			if len(fields) > 1 {
				defaultLabel = strings.ToLower(fields[1])
			}
		case "label":
			cur = &syslinuxStanza{}
			if len(fields) > 1 {
				cur.label = strings.ToLower(fields[1])
			}

			stanzas = append(stanzas, cur)
		case "kernel", "linux":
			if len(fields) > 1 {
				cur.kernelPath = resolveBootPath(configPath, fields[1])
			}
		case "initrd":
			if len(fields) > 1 {
				cur.initrdPath = resolveBootPath(configPath, fields[1])
			}
		case "append":
			cur.appendLine = rest
		}
	}

	for _, stanza := range stanzas {
		if stanza.label == defaultLabel && stanza.kernelPath != "" {
			return stanza.bootSpec(configPath)
		}
	}

	for _, stanza := range stanzas {
		if stanza.kernelPath != "" {
			return stanza.bootSpec(configPath)
		}
	}

	return bootSpec{}
}

func (s *syslinuxStanza) bootSpec(configPath string) bootSpec {
	initrdPath := s.initrdPath
	cmdline := s.appendLine

	if appendInitrd, cleaned := extractInitrdArg(cmdline); appendInitrd != "" {
		if initrdPath == "" {
			initrdPath = resolveBootPath(configPath, appendInitrd)
		}

		cmdline = cleaned
	}

	return bootSpec{
		kernelPath: s.kernelPath,
		initrdPath: initrdPath,
		cmdline:    cmdline,
	}
}

func parseGRUBConfig(configPath, data string) bootSpec {
	spec := bootSpec{}
	for _, line := range linesWithoutComments(data) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch strings.ToLower(fields[0]) {
		case "linux", "linuxefi", "linux16":
			if spec.kernelPath == "" && len(fields) > 1 {
				spec.kernelPath = resolveBootPath(configPath, fields[1])
				spec.cmdline = strings.Join(fields[2:], " ")
			}
		case "initrd", "initrdefi", "initrd16":
			if spec.initrdPath == "" && len(fields) > 1 {
				spec.initrdPath = resolveBootPath(configPath, fields[1])
			}
		}

		if spec.kernelPath != "" && spec.initrdPath != "" {
			return spec
		}
	}

	return spec
}

func linesWithoutComments(data string) []string {
	raw := strings.Split(data, "\n")
	lines := make([]string, 0, len(raw))

	for _, line := range raw {
		line = strings.TrimSpace(line)
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}

		if line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

func extractInitrdArg(cmdline string) (string, string) {
	fields := strings.Fields(cmdline)
	cleaned := make([]string, 0, len(fields))
	initrd := ""

	for _, field := range fields {
		value, ok := strings.CutPrefix(field, "initrd=")
		if !ok {
			cleaned = append(cleaned, field)

			continue
		}

		if i := strings.IndexByte(value, ','); i >= 0 {
			value = value[:i]
		}

		if initrd == "" {
			initrd = value
		}
	}

	if initrd == "" {
		return "", cmdline
	}

	return initrd, strings.Join(cleaned, " ")
}

func resolveBootPath(configPath, name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "\"'")
	name = strings.TrimPrefix(name, "cdrom:")

	if strings.HasPrefix(name, "/") {
		return path.Clean(name)
	}

	return path.Clean(path.Join(path.Dir(configPath), name))
}

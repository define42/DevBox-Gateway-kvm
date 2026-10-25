package gateway

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	bundledCodeName = "OVMF_CODE_4M.fd"
	bundledVarsName = "OVMF_VARS_4M.fd"
)

//go:embed firmware/OVMF_CODE_4M.fd firmware/OVMF_VARS_4M.fd firmware/LICENSE
var bundledFirmware embed.FS

// prepareManagerFirmware runs under the state-directory lock. The worker needs
// real firmware files for its read-only mmap and filesystem sandbox. Refresh
// only bundled assets; writable per-VM variable stores remain untouched.
func prepareManagerFirmware(dataDir string) error {
	dir := filepath.Join(dataDir, "firmware")
	//nolint:gosec // Fixed child of the private, exclusively owned state directory.
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create bundled firmware directory: %w", err)
	}
	if err := checkPrivateDirectory(dir); err != nil {
		return fmt.Errorf("check bundled firmware directory: %w", err)
	}
	for _, name := range []string{bundledCodeName, bundledVarsName, "LICENSE"} {
		data, err := bundledFirmware.ReadFile("firmware/" + name)
		if err != nil {
			return fmt.Errorf("read bundled firmware %s: %w", name, err)
		}
		if err := writeAtomic(dir, name, data); err != nil {
			return fmt.Errorf("write bundled firmware %s: %w", name, err)
		}
	}

	return nil
}

func (m *Manager) firmwarePath(name string) string {
	return filepath.Join(m.config.DataDir, "firmware", name)
}

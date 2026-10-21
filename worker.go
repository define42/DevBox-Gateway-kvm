package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
	"github.com/define42/DevBox-Gateway-kvm/internal/workersandbox"
)

var (
	errWorkerArguments = errors.New("worker requires boot arguments")
	errWorkerResources = errors.New("managed worker requires UEFI, disk, inherited RDP and userspace networking")
	errWorkerDiskSize  = errors.New("managed worker requires an explicit positive disk capacity")
)

// runWorker confines the process before any guest bytes are processed. Re-exec
// makes every Go and native thread inherit the same Landlock restrictions.
func runWorker(args []string) error {
	if len(args) == 0 || args[0] != "boot" {
		return errWorkerArguments
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	boot, _, err := flag.ParseArgs(append([]string{executable}, args...))
	if err != nil {
		return err
	}
	if err := validateWorkerBoot(boot); err != nil {
		return err
	}
	policy := workersandbox.Policy{
		ReadOnly:  []string{boot.UEFI, boot.CloudInit, boot.RDPCert, boot.RDPKey},
		ReadWrite: []string{boot.Disk, boot.UEFIVars},
		KVM:       boot.Dev,
		GPU:       boot.GPU3D || (boot.RDPH264 && boot.RDPH264Encoder != "software"),
	}
	if boot.ProfileSocket != "" {
		policy.SocketDir = filepath.Dir(boot.ProfileSocket)
	}
	workerArgs := append([]string{executable, "worker-boot"}, args[1:]...)
	if err := workersandbox.Exec(executable, workerArgs, workersandbox.Environment(os.Environ()), policy); err != nil {
		return fmt.Errorf("confine managed worker: %w", err)
	}

	return nil
}

func validateWorkerBoot(boot *flag.BootArgs) error {
	if boot == nil || boot.RDPFD == 0 || boot.UEFI == "" || boot.Disk == "" || boot.TapIfName != "" {
		return errWorkerResources
	}
	if boot.DiskSizeBytes <= 0 {
		return errWorkerDiskSize
	}

	return nil
}

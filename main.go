package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/define42/gokvm/flag"
	"github.com/define42/gokvm/machine"
	"github.com/define42/gokvm/probe"
	"github.com/define42/gokvm/vmm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args)
	stop()
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	bootArgs, probeArgs, err := flag.ParseArgs(args)
	if err != nil {
		return err
	}

	if bootArgs != nil {
		var h264Encoder string
		if bootArgs.RDPH264 {
			h264Encoder = bootArgs.RDPH264Encoder
		}
		c := &vmm.Config{
			Dev:            bootArgs.Dev,
			Kernel:         bootArgs.Kernel,
			Initrd:         bootArgs.Initrd,
			ISO:            bootArgs.ISO,
			UKI:            bootArgs.UKI,
			UEFI:           bootArgs.UEFI,
			UEFIVars:       bootArgs.UEFIVars,
			Params:         bootArgs.Params,
			ParamsSet:      bootArgs.ParamsSet,
			TapIfName:      bootArgs.TapIfName,
			Network:        bootArgs.Network,
			Disk:           bootArgs.Disk,
			GPU:            bootArgs.GPU,
			VNC:            bootArgs.VNC,
			RDP:            bootArgs.RDP,
			RDPWait:        bootArgs.RDPWait,
			RDPCert:        bootArgs.RDPCert,
			RDPKey:         bootArgs.RDPKey,
			RDPH264:        bootArgs.RDPH264,
			RDPH264Encoder: h264Encoder,
			RDPH264Threads: bootArgs.RDPH264Threads,
			RDPStats:       bootArgs.RDPStats,
			Audio:          bootArgs.Audio,
			NCPUs:          bootArgs.NCPUs,
			MemSize:        bootArgs.MemSize,
			TraceCount:     bootArgs.TraceCount,
		}

		return runBoot(ctx, *c)
	}

	if probeArgs != nil {
		if err := probe.KVMCapabilities(); err != nil {
			return err
		}

		if err := probe.CPUID(); err != nil {
			return err
		}
	}

	return nil
}

// runBoot recreates the complete VM on a firmware-requested reset. Closing the
// previous instance first flushes disks/VARS and releases their exclusive locks.
func runBoot(ctx context.Context, conf vmm.Config) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := bootGuest(ctx, conf)
		if conf.UEFI == "" || !isGuestReset(err) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Print("UEFI guest requested a reset; restarting with the saved firmware variables")
	}
}

func bootGuest(ctx context.Context, conf vmm.Config) (bootErr error) {
	vm := vmm.New(conf)
	defer func() { bootErr = errors.Join(bootErr, vm.Close()) }()
	if err := vm.Init(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := vm.Setup(); err != nil {
		return err
	}

	return vm.BootContext(ctx)
}

// isGuestReset accepts wrapped reset errors but never drops an additional
// shutdown, persistence, or cancellation error joined with the reset request.
func isGuestReset(err error) bool {
	if err == nil {
		return false
	}
	// Inspect only the current node; errors.As could skip other joined failures.
	switch wrapped := err.(type) { //nolint:errorlint
	case interface{ Unwrap() []error }:
		children := wrapped.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isGuestReset(child) {
				return false
			}
		}

		return true
	case interface{ Unwrap() error }:
		return isGuestReset(wrapped.Unwrap())
	default:
		return errors.Is(err, machine.ErrUEFIReset)
	}
}

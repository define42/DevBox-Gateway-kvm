package main

import (
	"context"
	"errors"
	stdflag "flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
	"github.com/define42/DevBox-Gateway-kvm/internal/probe"
	"github.com/define42/DevBox-Gateway-kvm/internal/profiling"
	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
	"github.com/define42/DevBox-Gateway-kvm/internal/vmm"
	"golang.org/x/sys/unix"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args)
	stop()
	if err != nil && !errors.Is(err, stdflag.ErrHelp) && !isOnlyError(err, context.Canceled) {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) > 1 {
		switch args[1] {
		case "serve":
			return runServe(ctx, args[2:], os.Stderr)
		case "seed":
			return runSeed(args[2:], os.Stdin, os.Stdout)
		case "auth-user":
			return runAuthUser(args[2:], os.Stdin, os.Stdout)
		}
	}
	bootArgs, probeArgs, err := flag.ParseArgs(args)
	if err != nil {
		return err
	}

	if bootArgs == nil && probeArgs == nil {
		return nil
	}
	if bootArgs == nil {
		return probeHost()
	}

	var h264Encoder, rdpProfile string
	if bootArgs.RDP != "" {
		rdpProfile = bootArgs.RDPProfile
	}
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
		CloudInit:      bootArgs.CloudInit,
		GPU:            bootArgs.GPU,
		VNC:            bootArgs.VNC,
		RDP:            bootArgs.RDP,
		RDPProfile:     rdpProfile,
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

	if bootArgs.RDPFD != 0 {
		// Keep the inherited socket reserved across firmware-requested resets.
		// Each VMM duplicates it for its own display lifecycle.
		if _, err := unix.FcntlInt(uintptr(bootArgs.RDPFD), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			return fmt.Errorf("adopt inherited RDP listener: %w", err)
		}
		c.RDPListenerFile = os.NewFile(uintptr(bootArgs.RDPFD), "inherited-rdp-listener")
		defer c.RDPListenerFile.Close()
	}

	if bootArgs.TelemetryFD != 0 {
		collector, stop, telemetryErr := telemetry.Start(ctx, bootArgs.TelemetryFD)
		if telemetryErr != nil {
			// Diagnostics must never make an otherwise valid VM unavailable.
			log.Printf("VM telemetry disabled: %v", telemetryErr)
		} else {
			defer stop()
			c.Telemetry = collector
		}
	}

	if bootArgs.ProfileSocket != "" {
		stop, profileErr := profiling.Start(ctx, bootArgs.ProfileSocket)
		if profileErr != nil {
			log.Printf("VM CPU profiling disabled: %v", profileErr)
		} else {
			defer stop()
		}
	}

	return runBoot(ctx, *c)
}

func probeHost() error {
	if err := probe.KVMCapabilities(); err != nil {
		return err
	}

	return probe.CPUID()
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
	power := make(chan os.Signal, 1)
	signal.Notify(power, syscall.SIGUSR1)
	defer signal.Stop(power)
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

	controlCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-controlCtx.Done():
				return
			case <-power:
				if err := vm.RequestShutdown(); err != nil {
					log.Printf("Guest power-button request failed: %v", err)
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()

	return vm.BootContext(ctx)
}

// isGuestReset accepts wrapped reset errors but never drops an additional
// shutdown, persistence, or cancellation error joined with the reset request.
func isGuestReset(err error) bool {
	return isOnlyError(err, machine.ErrUEFIReset)
}

// isOnlyError never hides unrelated cleanup failures joined with a benign exit.
func isOnlyError(err, target error) bool {
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
			if !isOnlyError(child, target) {
				return false
			}
		}

		return true
	case interface{ Unwrap() error }:
		return isOnlyError(wrapped.Unwrap(), target)
	default:
		return errors.Is(err, target)
	}
}

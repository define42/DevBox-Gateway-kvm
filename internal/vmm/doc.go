// Package vmm coordinates a single KVM guest's boot and resource lifecycle.
// It supports local Linux/PVH kernel boot and UEFI disk boot, with optional
// cloud-init seed media and gateway-provided RDP input, display and playback audio.
//
// Construct a VMM with New, call Init and Setup, then Boot or BootContext.
// Boot closes owned resources on return. Firmware resets require a new VMM;
// borrowed process-level resources remain with the caller across resets.
package vmm

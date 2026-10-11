package iso9660

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestLoadBootFilesFromTinyCoreStyleISO(t *testing.T) {
	t.Parallel()

	const (
		rootSector     = 20
		bootSector     = 21
		isolinuxSector = 22
		kernelSector   = 23
		initrdSector   = 24
		configSector   = 25
	)

	kernel := []byte("tinycore kernel")
	initrd := []byte("tinycore initrd")
	config := []byte("DEFAULT tinycore\n" +
		"LABEL tinycore\n" +
		"KERNEL /boot/vmlinuz\n" +
		"APPEND initrd=/boot/core.gz loglevel=3 cde\n")

	isolinuxDir := joinRecords(
		dirRecord([]byte{0}, isolinuxSector, 0, true),
		dirRecord([]byte{1}, bootSector, 0, true),
		dirRecord([]byte("ISOLINUX.CFG;1"), configSector, len(config), false),
	)
	isolinuxDir = joinRecords(
		dirRecord([]byte{0}, isolinuxSector, len(isolinuxDir), true),
		dirRecord([]byte{1}, bootSector, 0, true),
		dirRecord([]byte("ISOLINUX.CFG;1"), configSector, len(config), false),
	)

	bootDir := joinRecords(
		dirRecord([]byte{0}, bootSector, 0, true),
		dirRecord([]byte{1}, rootSector, 0, true),
		dirRecord([]byte("VMLINUZ.;1"), kernelSector, len(kernel), false),
		dirRecord([]byte("CORE.GZ;1"), initrdSector, len(initrd), false),
		dirRecord([]byte("ISOLINUX"), isolinuxSector, len(isolinuxDir), true),
	)
	bootDir = joinRecords(
		dirRecord([]byte{0}, bootSector, len(bootDir), true),
		dirRecord([]byte{1}, rootSector, 0, true),
		dirRecord([]byte("VMLINUZ.;1"), kernelSector, len(kernel), false),
		dirRecord([]byte("CORE.GZ;1"), initrdSector, len(initrd), false),
		dirRecord([]byte("ISOLINUX"), isolinuxSector, len(isolinuxDir), true),
	)

	rootDir := joinRecords(
		dirRecord([]byte{0}, rootSector, 0, true),
		dirRecord([]byte{1}, rootSector, 0, true),
		dirRecord([]byte("BOOT"), bootSector, len(bootDir), true),
	)
	rootDir = joinRecords(
		dirRecord([]byte{0}, rootSector, len(rootDir), true),
		dirRecord([]byte{1}, rootSector, len(rootDir), true),
		dirRecord([]byte("BOOT"), bootSector, len(bootDir), true),
	)

	iso := make([]byte, sectorSize*32)
	writePrimaryVolumeDescriptor(iso[sectorSize*16:sectorSize*17], rootSector, len(rootDir))
	writeVolumeDescriptorTerminator(iso[sectorSize*17 : sectorSize*18])
	copySector(iso, rootSector, rootDir)
	copySector(iso, bootSector, bootDir)
	copySector(iso, isolinuxSector, isolinuxDir)
	copySector(iso, kernelSector, kernel)
	copySector(iso, initrdSector, initrd)
	copySector(iso, configSector, config)

	reader, err := NewReader(bytes.NewReader(iso), int64(len(iso)))
	if err != nil {
		t.Fatal(err)
	}

	files, err := LoadBootFiles(reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(files.Kernel, kernel) {
		t.Fatalf("kernel: got %q, want %q", files.Kernel, kernel)
	}

	if !bytes.Equal(files.Initrd, initrd) {
		t.Fatalf("initrd: got %q, want %q", files.Initrd, initrd)
	}

	if files.KernelPath != "/boot/vmlinuz" {
		t.Fatalf("kernel path: got %q", files.KernelPath)
	}

	if files.InitrdPath != "/boot/core.gz" {
		t.Fatalf("initrd path: got %q", files.InitrdPath)
	}

	if files.Cmdline != "loglevel=3 cde" {
		t.Fatalf("cmdline: got %q", files.Cmdline)
	}
}

func TestLoadBootFilesFromElToritoSyslinuxISO(t *testing.T) {
	t.Parallel()

	const (
		catalogSector   = 18
		rootSector      = 20
		slaxSector      = 21
		bootSector      = 22
		bootImageSector = 23
		kernelSector    = 24
		initrdSector    = 25
		configSector    = 26
	)

	kernel := []byte("slax kernel")
	initrd := []byte("slax initrd")
	bootImage := []byte("isolinux boot image")
	config := []byte("UI /slax/boot/vesamenu.c32\n" +
		"LABEL default\n" +
		"MENU LABEL Run Slax from CD\n" +
		"KERNEL /slax/boot/vmlinuz\n" +
		"APPEND vga=normal initrd=/slax/boot/initrfs.img load_ramdisk=1 prompt_ramdisk=0 rw " +
		"printk.time=0 consoleblank=0 automount\n")

	bootDir := joinRecords(
		dirRecord([]byte{0}, bootSector, 0, true),
		dirRecord([]byte{1}, slaxSector, 0, true),
		dirRecord([]byte("ISOLINUX.BIN;1"), bootImageSector, len(bootImage), false),
		dirRecord([]byte("VMLINUZ.;1"), kernelSector, len(kernel), false),
		dirRecord([]byte("INITRFS.IMG;1"), initrdSector, len(initrd), false),
		dirRecord([]byte("ISOLINUX.CFG;1"), configSector, len(config), false),
	)
	bootDir = joinRecords(
		dirRecord([]byte{0}, bootSector, len(bootDir), true),
		dirRecord([]byte{1}, slaxSector, 0, true),
		dirRecord([]byte("ISOLINUX.BIN;1"), bootImageSector, len(bootImage), false),
		dirRecord([]byte("VMLINUZ.;1"), kernelSector, len(kernel), false),
		dirRecord([]byte("INITRFS.IMG;1"), initrdSector, len(initrd), false),
		dirRecord([]byte("ISOLINUX.CFG;1"), configSector, len(config), false),
	)

	slaxDir := joinRecords(
		dirRecord([]byte{0}, slaxSector, 0, true),
		dirRecord([]byte{1}, rootSector, 0, true),
		dirRecord([]byte("BOOT"), bootSector, len(bootDir), true),
	)
	slaxDir = joinRecords(
		dirRecord([]byte{0}, slaxSector, len(slaxDir), true),
		dirRecord([]byte{1}, rootSector, 0, true),
		dirRecord([]byte("BOOT"), bootSector, len(bootDir), true),
	)

	rootDir := joinRecords(
		dirRecord([]byte{0}, rootSector, 0, true),
		dirRecord([]byte{1}, rootSector, 0, true),
		dirRecord([]byte("SLAX"), slaxSector, len(slaxDir), true),
	)
	rootDir = joinRecords(
		dirRecord([]byte{0}, rootSector, len(rootDir), true),
		dirRecord([]byte{1}, rootSector, len(rootDir), true),
		dirRecord([]byte("SLAX"), slaxSector, len(slaxDir), true),
	)

	iso := make([]byte, sectorSize*32)
	writePrimaryVolumeDescriptor(iso[sectorSize*16:sectorSize*17], rootSector, len(rootDir))
	writeElToritoBootRecord(iso[sectorSize*17:sectorSize*18], catalogSector)
	writeElToritoBootCatalog(iso[sectorSize*catalogSector:sectorSize*(catalogSector+1)], bootImageSector)
	writeVolumeDescriptorTerminator(iso[sectorSize*19 : sectorSize*20])
	copySector(iso, rootSector, rootDir)
	copySector(iso, slaxSector, slaxDir)
	copySector(iso, bootSector, bootDir)
	copySector(iso, bootImageSector, bootImage)
	copySector(iso, kernelSector, kernel)
	copySector(iso, initrdSector, initrd)
	copySector(iso, configSector, config)

	reader, err := NewReader(bytes.NewReader(iso), int64(len(iso)))
	if err != nil {
		t.Fatal(err)
	}

	files, err := LoadBootFiles(reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(files.Kernel, kernel) {
		t.Fatalf("kernel: got %q, want %q", files.Kernel, kernel)
	}

	if !bytes.Equal(files.Initrd, initrd) {
		t.Fatalf("initrd: got %q, want %q", files.Initrd, initrd)
	}

	if files.KernelPath != "/slax/boot/vmlinuz" {
		t.Fatalf("kernel path: got %q", files.KernelPath)
	}

	if files.InitrdPath != "/slax/boot/initrfs.img" {
		t.Fatalf("initrd path: got %q", files.InitrdPath)
	}

	wantCmdline := "vga=normal load_ramdisk=1 prompt_ramdisk=0 rw printk.time=0 consoleblank=0 automount"
	if files.Cmdline != wantCmdline {
		t.Fatalf("cmdline: got %q, want %q", files.Cmdline, wantCmdline)
	}
}

func TestLoadBootFilesFromExternalISO(t *testing.T) {
	t.Parallel()

	name := os.Getenv("GOKVM_TEST_ISO")
	if name == "" {
		t.Skip("set GOKVM_TEST_ISO to an ISO image path")
	}

	file, err := os.Open(name) //nolint:gosec // Opt-in test path from GOKVM_TEST_ISO.
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}

	files, err := LoadBootFiles(reader)
	if err != nil {
		t.Fatal(err)
	}

	if len(files.Kernel) == 0 {
		t.Fatal("kernel is empty")
	}

	t.Logf("kernel=%s initrd=%s cmdline=%q", files.KernelPath, files.InitrdPath, files.Cmdline)
}

func writePrimaryVolumeDescriptor(dst []byte, rootExtent, rootSize int) {
	dst[0] = 1
	copy(dst[1:6], "CD001")
	dst[6] = 1
	copy(dst[156:], dirRecord([]byte{0}, rootExtent, rootSize, true))
}

func writeVolumeDescriptorTerminator(dst []byte) {
	dst[0] = 255
	copy(dst[1:6], "CD001")
	dst[6] = 1
}

func writeElToritoBootRecord(dst []byte, catalogSector int) {
	dst[0] = 0
	copy(dst[1:6], "CD001")
	dst[6] = 1
	copy(dst[7:39], "EL TORITO SPECIFICATION")
	binary.LittleEndian.PutUint32(dst[71:75], uint32(catalogSector))
}

func writeElToritoBootCatalog(dst []byte, bootImageSector int) {
	validation := dst[:32]
	validation[0] = 0x01
	validation[1] = 0x00
	validation[30] = 0x55
	validation[31] = 0xaa

	var sum uint16
	for i := 0; i < len(validation); i += 2 {
		sum += binary.LittleEndian.Uint16(validation[i : i+2])
	}
	binary.LittleEndian.PutUint16(validation[28:30], 0-sum)

	initialEntry := dst[32:64]
	initialEntry[0] = 0x88
	initialEntry[1] = 0x00
	binary.LittleEndian.PutUint16(initialEntry[6:8], 4)
	binary.LittleEndian.PutUint32(initialEntry[8:12], uint32(bootImageSector))
}

func dirRecord(name []byte, extent, size int, isDir bool) []byte {
	recLen := 33 + len(name)
	if recLen%2 != 0 {
		recLen++
	}

	rec := make([]byte, recLen)
	rec[0] = byte(recLen)
	binary.LittleEndian.PutUint32(rec[2:6], uint32(extent))
	binary.BigEndian.PutUint32(rec[6:10], uint32(extent))
	binary.LittleEndian.PutUint32(rec[10:14], uint32(size))
	binary.BigEndian.PutUint32(rec[14:18], uint32(size))
	if isDir {
		rec[25] = 0x02
	}

	rec[28] = 1
	binary.BigEndian.PutUint16(rec[30:32], 1)
	rec[32] = byte(len(name))
	copy(rec[33:], name)

	return rec
}

func joinRecords(records ...[]byte) []byte {
	var out []byte
	for _, record := range records {
		out = append(out, record...)
	}

	return out
}

func copySector(iso []byte, sector int, data []byte) {
	copy(iso[sector*sectorSize:], data)
}

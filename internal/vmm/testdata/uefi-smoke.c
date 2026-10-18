/* A self-contained disk boot probe: no Linux kernel or initrd is involved. */
#include <efi.h>
#include <efilib.h>

static void serial_write(unsigned short port, unsigned char value)
{
    __asm__ volatile ("outb %0, %1" : : "a"(value), "Nd"(port));
}

EFI_STATUS efi_main(EFI_HANDLE image, EFI_SYSTEM_TABLE *system)
{
    const char marker[] = "GOKVM_QCOW2_UEFI_BOOT_OK\r\n";
    (void)image;

    /* COM1, 115200 baud, 8N1; bypass firmware console redirection. */
    serial_write(0x3f9, 0);
    serial_write(0x3fb, 0x80);
    serial_write(0x3f8, 1);
    serial_write(0x3f9, 0);
    serial_write(0x3fb, 3);
    for (unsigned int i = 0; i < sizeof(marker) - 1; i++)
        serial_write(0x3f8, marker[i]);

    uefi_call_wrapper(system->RuntimeServices->ResetSystem, 4,
                      EfiResetShutdown, EFI_SUCCESS, 0, NULL);
    return EFI_ABORTED; /* ResetSystem must not return. */
}

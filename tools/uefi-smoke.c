/* Opt-in native firmware smoke fixture, built by test-uefi.sh with GNU-EFI. */
#include <efi.h>
#include <efilib.h>

static void marker(const char *message)
{
    for (; *message; ++message) {
        __asm__ volatile("outb %0, %1" : : "a"(*message), "Nd"((unsigned short)0x402));
    }
}

static EFI_STATUS fail(const char *message)
{
    marker("GOKVM TEST FAILED: ");
    marker(message);
    marker("\n");
    return EFI_ABORTED;
}

EFI_STATUS efi_main(EFI_HANDLE image, EFI_SYSTEM_TABLE *system)
{
    static EFI_GUID counter_guid = {
        0x10293847, 0x5647, 0x4abc, {0x8d, 0x10, 0x31, 0x22, 0x19, 0xfa, 0x8b, 0x02}
    };
    EFI_GRAPHICS_OUTPUT_PROTOCOL *graphics = NULL;
    EFI_GRAPHICS_OUTPUT_BLT_PIXEL pixel = {0x30, 0xa0, 0x20, 0};
    UINT32 counter = 0;
    UINTN size = sizeof(counter);
    EFI_STATUS status;

    InitializeLib(image, system);
    marker("GOKVM TEST EFI ENTRY\n");
    status = uefi_call_wrapper(system->BootServices->LocateProtocol, 3,
                              &GraphicsOutputProtocol, NULL, (VOID **)&graphics);
    if (EFI_ERROR(status) || graphics == NULL || graphics->Mode == NULL ||
        graphics->Mode->Info == NULL) {
        return fail("no graphics output protocol");
    }
    status = uefi_call_wrapper(graphics->Blt, 10, graphics, &pixel, EfiBltVideoFill,
                              0, 0, 0, 0, 32, 32, 0);
    if (EFI_ERROR(status)) {
        return fail("graphics blit");
    }
    marker("GOKVM TEST GRAPHICS OK\n");
    Print(L"GoKVM native UEFI disk boot test\r\nGraphics output is working.\r\n");

    status = uefi_call_wrapper(system->RuntimeServices->GetVariable, 5,
                              L"GokvmSmokeCounter", &counter_guid, NULL, &size, &counter);
    if (status == EFI_NOT_FOUND) {
        counter = 0;
    } else if (EFI_ERROR(status) || size != sizeof(counter) || counter != 1) {
        return fail("unexpected persistent counter");
    }
    if (counter == 1) {
        marker("GOKVM TEST VARS PERSISTED\n");
    }
    ++counter;
    status = uefi_call_wrapper(system->RuntimeServices->SetVariable, 5,
                              L"GokvmSmokeCounter", &counter_guid,
                              EFI_VARIABLE_NON_VOLATILE | EFI_VARIABLE_BOOTSERVICE_ACCESS |
                              EFI_VARIABLE_RUNTIME_ACCESS, sizeof(counter), &counter);
    if (EFI_ERROR(status)) {
        return fail("variable write");
    }
    marker("GOKVM TEST VARS WRITE OK\n");
    Print(L"Persistent boot counter: %d\r\n", counter);
    uefi_call_wrapper(system->BootServices->Stall, 1, 250000);
    if (counter == 1) {
        marker("GOKVM TEST RESET\n");
        uefi_call_wrapper(system->RuntimeServices->ResetSystem, 4,
                          EfiResetCold, EFI_SUCCESS, 0, NULL);
    } else {
        marker("GOKVM TEST SHUTDOWN\n");
        uefi_call_wrapper(system->RuntimeServices->ResetSystem, 4,
                          EfiResetShutdown, EFI_SUCCESS, 0, NULL);
    }
    return fail("ResetSystem returned");
}

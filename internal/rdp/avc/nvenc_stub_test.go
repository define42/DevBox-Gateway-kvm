//go:build !nvenc || !cgo

package avc

import (
	"errors"
	"strings"
	"testing"
)

func TestNVENCUnavailableBuild(t *testing.T) {
	t.Parallel()
	encoder, err := NewEncoderWithOptions(64, 48, Options{Threads: 1, Backend: BackendNVENC})
	if encoder != nil || !errors.Is(err, ErrCodec) || !strings.Contains(err.Error(), "-tags nvenc") {
		t.Fatalf("explicit NVENC without native build = %v, %v", encoder, err)
	}
	encoder, err = NewEncoderWithOptions(64, 48, Options{Threads: 1, Backend: BackendAuto})
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	if encoder.Backend() != BackendSoftware || !errors.Is(encoder.FallbackReason(), ErrCodec) {
		t.Fatalf("auto without native build: backend %s, reason %v", encoder.Backend(), encoder.FallbackReason())
	}
}

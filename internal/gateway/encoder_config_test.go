package gateway

import (
	"errors"
	"testing"
)

func TestManagerH264EncoderConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		encoder string
		wantErr error
	}{
		{name: "default"},
		{name: "software", encoder: "software"},
		{name: "auto alias", encoder: "auto"},
		{name: "removed nvenc", encoder: "nvenc", wantErr: ErrInvalid},
		{name: "unknown", encoder: "unknown", wantErr: ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := ManagerConfig{Encoder: test.encoder}
			err := normalizeManagerLimits(&config)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("normalization error = %v, want %v", err, test.wantErr)
			}
			if err == nil && config.Encoder != "software" {
				t.Fatalf("normalized encoder = %q, want software", config.Encoder)
			}
		})
	}
}

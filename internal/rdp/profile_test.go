package rdp

import (
	"errors"
	"testing"
)

func TestProfileFrameRate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, profile string
		want          int
	}{
		{name: "compatible default", want: 60},
		{name: "office", profile: ProfileOffice, want: 30},
		{name: "smooth", profile: ProfileSmooth, want: 60},
		{name: "unknown", profile: "fast"},
		{name: "case sensitive", profile: "Office"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProfileFrameRate(test.profile)
			if got != test.want || (test.want == 0 && !errors.Is(err, ErrProfile)) || (test.want != 0 && err != nil) {
				t.Fatalf("ProfileFrameRate(%q) = %d, %v; want %d", test.profile, got, err, test.want)
			}
		})
	}
}

package rdp

import "errors"

const (
	// ProfileOffice caps full-picture encoding at 30 FPS for desktop density.
	ProfileOffice = "office"
	// ProfileSmooth caps full-picture encoding at 30 FPS.
	ProfileSmooth = "smooth"
)

// ErrProfile indicates an unsupported RDP performance profile.
var ErrProfile = errors.New("RDP profile must be office or smooth")

// ProfileFrameRate returns the maximum graphics frame rate of a profile.
// Empty preserves the standalone display's historical 60-FPS behavior.
// Bitmap clients retain their existing, lower 30-FPS ceiling.
func ProfileFrameRate(profile string) (int, error) {
	switch profile {
	case ProfileOffice, ProfileSmooth:
		return 30, nil
	case "":
		return 60, nil
	default:
		return 0, ErrProfile
	}
}

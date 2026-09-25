package config

// Whether image signatures are verified — before a base image is recommended,
// before an image is pulled, and during a scan (§3.82).
//
// A string, not a bool: false is a bool's zero value, so every file written
// before the key existed would decode to "do not verify" (D12). Only "off"
// turns it off; anything else — a typo included — verifies, the safe side.
const (
	ImageVerificationOn  = "on"
	ImageVerificationOff = "off"
)

// ImageVerifications is the set scan.image_verification cycles through in the
// configuration view.
func ImageVerifications() []string {
	return []string{ImageVerificationOn, ImageVerificationOff}
}

// VerifiesImages reports whether signatures are verified.
func (c ScanConfig) VerifiesImages() bool { return c.ImageVerification != ImageVerificationOff }

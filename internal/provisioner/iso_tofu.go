package provisioner

import "strings"

// tofuResult is the decision outcome of comparing a freshly-downloaded ISO's
// hash against the trust-on-first-use value stored as a ZFS user property.
type tofuResult int

const (
	// tofuFirstUse: no stored hash yet. The caller should record the
	// downloaded hash and proceed.
	tofuFirstUse tofuResult = iota

	// tofuMatch: stored hash equals downloaded hash. Proceed normally.
	tofuMatch

	// tofuMismatch: stored hash disagrees with downloaded hash. The caller
	// should POISON-mark the stored value and fail the provision.
	tofuMismatch

	// tofuPoisoned: the stored hash was already POISON-marked by a prior
	// mismatch. Fail immediately; do not upload.
	tofuPoisoned
)

// poisonedPrefix marks a stored hash as tainted by a prior mismatch.
// Kept as a package-level const so tests can reference the exact shape.
const poisonedPrefix = "POISONED-"

// classifyTOFU maps a stored/downloaded pair to the TOFU decision outcome.
// Extracted from stepUploadISO so the decision table is unit-testable
// without a working HTTP server and TrueNAS mock.
func classifyTOFU(storedHash, downloadedHash string) tofuResult {
	if strings.HasPrefix(storedHash, poisonedPrefix) {
		return tofuPoisoned
	}

	if storedHash == "" {
		return tofuFirstUse
	}

	if storedHash == downloadedHash {
		return tofuMatch
	}

	return tofuMismatch
}

// cachedISOPoisoned returns true iff the stored hash indicates the cached
// ISO was tainted by a prior mismatch. Used on cache hits when no fresh
// download has happened — we only read, not compare.
func cachedISOPoisoned(storedHash string) bool {
	return strings.HasPrefix(storedHash, poisonedPrefix)
}

// poisonMarker formats a POISON-tagged stored value carrying the mismatched
// hash that triggered the poison. Exposed so tests can assert on format.
func poisonMarker(badHash string) string {
	return poisonedPrefix + badHash
}

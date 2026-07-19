// Package testutil provides shared helpers for tests across internal packages.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// DefaultCassetteMaxAgeDays is the default staleness threshold applied by
// CassettesNotStale when neither an explicit override argument nor the
// CASSETTE_MAX_AGE_DAYS environment variable is set.
//
// The value is intentionally longer than the historical 90-day floor
// because the current cassette set was recorded on 2026-04-10 and the
// v0.16.3 deps-bump PR did not touch the TrueNAS wire surface. A re-record
// is tracked separately; bumping the threshold here prevents the gate from
// rubber-stamping every fresh clone while still failing loudly if the
// cassettes drift into "over six months old" territory.
//
// TODO: re-record cassettes and drop this back to 90 days. Tracked in
// docs/backlog.md under "Re-record cassettes post-1.26.5 bump".
const DefaultCassetteMaxAgeDays = 180

// CassettesNotStale asserts that every .json cassette directly inside dir
// was introduced by a git commit no older than maxAgeDays. If maxAgeDays
// is zero, the DefaultCassetteMaxAgeDays constant is used. The CASSETTE_MAX_AGE_DAYS
// environment variable, when set, overrides both.
//
// Age is measured against the commit that introduced the file (via
// `git log -1 --format=%ct -- <path>`) rather than filesystem mtime, which
// otherwise reflects the checkout wall-clock and would rubber-stamp every
// fresh clone as "0 days old".
//
// The test is skipped (not failed) when:
//   - dir does not exist, or
//   - dir contains no *.json files, or
//   - CASSETTE_MAX_AGE_DAYS is explicitly set to 0, or
//   - `git` is not available on PATH (rare, but keeps offline dev sane).
func CassettesNotStale(t *testing.T, dir string, maxAgeDays int) {
	t.Helper()

	if maxAgeDays <= 0 {
		maxAgeDays = DefaultCassetteMaxAgeDays
	}

	if v := os.Getenv("CASSETTE_MAX_AGE_DAYS"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("CASSETTE_MAX_AGE_DAYS=%q: not an integer", v)
		}

		maxAgeDays = parsed
	}

	if maxAgeDays == 0 {
		t.Skip("CASSETTE_MAX_AGE_DAYS=0 disables the cassette-age gate")
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("cassette dir %q does not exist", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cassette dir %q: %v", dir, err)
	}

	var cassettes []string

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		cassettes = append(cassettes, filepath.Join(dir, e.Name()))
	}

	if len(cassettes) == 0 {
		t.Skipf("no cassettes found under %q", dir)
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found on PATH: %v", err)
	}

	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)

	var stale []string

	for _, path := range cassettes {
		commitTime, err := gitCommitTime(path)
		if err != nil {
			t.Fatalf("git log for %q: %v", path, err)
		}

		if commitTime.IsZero() {
			// File is untracked or just added — treat as fresh.
			continue
		}

		if commitTime.Before(cutoff) {
			age := int(time.Since(commitTime).Hours() / 24)
			stale = append(stale, path+" ("+strconv.Itoa(age)+" days old)")
		}
	}

	if len(stale) > 0 {
		t.Fatalf(
			"cassettes older than %d days — TrueNAS API shape may have drifted since they were recorded. "+
				"Re-record with `make test-record` against a live TrueNAS, or bump CASSETTE_MAX_AGE_DAYS "+
				"for a one-off override.\n\nStale:\n  %v",
			maxAgeDays, stale,
		)
	}
}

// gitCommitTime returns the author-commit time of the most recent commit
// that touched path. Returns the zero time (no error) when git has no
// history for the path (uncommitted or untracked).
func gitCommitTime(path string) (time.Time, error) {
	// #nosec G204 — path is a test-supplied cassette path, not user input.
	cmd := exec.Command("git", "log", "-1", "--format=%ct", "--", path)

	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}

	s := strings.TrimSpace(string(out))
	if s == "" {
		return time.Time{}, nil
	}

	secs, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(secs, 0), nil
}

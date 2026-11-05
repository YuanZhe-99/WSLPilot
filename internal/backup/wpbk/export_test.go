package wpbk

import "testing"

// SampleTar exposes the metadata-rich test tar to the external spec test.
func SampleTar(tb testing.TB) []byte { return buildTar(tb, sampleEntries(1)) }

// CompareTarBytes exposes the field-by-field tar comparison to the external
// spec test.
func CompareTarBytes(tb testing.TB, want, got []byte) {
	tb.Helper()
	compareTars(tb, parseTar(tb, want), parseTar(tb, got))
}

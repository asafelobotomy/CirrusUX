package catalogue

import (
	"strings"
	"testing"

	"cirrusux/internal/version"
)

func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = prev })
}

func withMin(min, extra string) string {
	return strings.Replace(good, `"group":"Apps"`, `"group":"Apps","minVersion":"`+min+`"`+extra, 1)
}

func TestMinVersionNewerGivesTypedErrorEvenWithUnknownFields(t *testing.T) {
	withVersion(t, "0.7.0")
	_, err := Parse([]byte(withMin("0.8.0", `,"someFutureField":{"x":1}`)))
	e, ok := AsNeedsNewer(err)
	if !ok || e.Min != "0.8.0" {
		t.Fatalf("got %v, want ErrNeedsNewer 0.8.0", err)
	}
	want := "This app needs CirrusUX 0.8.0 or newer: run sudo cirrusux-setup upgrade on the server."
	if err.Error() != want {
		t.Errorf("message %q", err)
	}
}

func TestMinVersionEqualOrOlderParsesStrictly(t *testing.T) {
	withVersion(t, "0.7.0")
	for _, min := range []string{"0.7.0", "0.6.9", "0.1.0"} {
		a, err := Parse([]byte(withMin(min, "")))
		if err != nil || a.MinVersion != min {
			t.Errorf("minVersion %s: %v", min, err)
		}
		_, err = Parse([]byte(withMin(min, `,"someFutureField":1`)))
		if err == nil {
			t.Errorf("minVersion %s: unknown field accepted", min)
		} else if _, ok := AsNeedsNewer(err); ok {
			t.Errorf("minVersion %s: wrong error %v", min, err)
		}
	}
}

func TestMinVersionShapeIsChecked(t *testing.T) {
	withVersion(t, "0.7.0")
	for _, min := range []string{"v0.8.0", "0.8", "latest", "0.8.0.1", "1.x.0"} {
		_, err := Parse([]byte(withMin(min, "")))
		if err == nil {
			t.Errorf("minVersion %q accepted", min)
		} else if _, ok := AsNeedsNewer(err); ok {
			t.Errorf("minVersion %q: %v", min, err)
		}
	}
}

func TestDevBuildAcceptsEveryMinVersion(t *testing.T) {
	withVersion(t, "dev")
	if _, err := Parse([]byte(withMin("99.0.0", ""))); err != nil {
		t.Errorf("dev build refused: %v", err)
	}
}

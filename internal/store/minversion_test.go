package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/version"
)

func atVersion(t *testing.T, v string) {
	t.Helper()
	prev := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = prev })
}

// futureEnvelope signs an envelope around a definition a newer CirrusUX
// wrote: minVersion 0.8.0 and a field this version doesn't know.
func futureEnvelope(t *testing.T, sec Secret, source string) Signed {
	t.Helper()
	var app map[string]any
	if err := json.Unmarshal(def(t, "jellyfin"), &app); err != nil {
		t.Fatal(err)
	}
	app["minVersion"], app["fromTheFuture"] = "0.8.0", true
	raw, _ := json.Marshal(app)
	env, _ := json.Marshal(map[string]any{"source": source, "serial": 4, "expires": now.Add(time.Hour), "app": json.RawMessage(raw)})
	return Signed{Data: env, Sig: string(sec.Sign(env))}
}

func TestOpenAppNeedsNewerCirrusUX(t *testing.T) {
	atVersion(t, "0.7.0")
	k, sec := testKey(t, "example")
	_, other := testKey(t, "other")
	src := Source{Name: "example", Keys: []Key{k}}

	a, err := src.OpenApp("jellyfin", futureEnvelope(t, sec, "example"), now, 0)
	need, ok := catalogue.AsNeedsNewer(err)
	if !ok || need.Min != "0.8.0" || a.Serial != 4 || a.Source != "example" {
		t.Fatalf("OpenApp = %+v, %v", a, err)
	}

	// The signature and the header are still checked first.
	for name, c := range map[string]struct {
		f    Signed
		want error
	}{
		"wrong key":    {futureEnvelope(t, other, "example"), ErrSignature},
		"other source": {futureEnvelope(t, sec, "elsewhere"), ErrSource},
	} {
		_, err := src.OpenApp("jellyfin", c.f, now, 0)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
	if _, err := src.OpenApp("jellyfin", futureEnvelope(t, sec, "example"), now, 5); !errors.Is(err, ErrReplay) {
		t.Errorf("replay: %v", err)
	}
	if _, err := src.OpenApp("jellyfin", futureEnvelope(t, sec, "example"), now.Add(2*time.Hour), 0); !errors.Is(err, ErrExpired) {
		t.Errorf("expired: %v", err)
	}

	// A dev build accepts the field it knows but still refuses the unknown one.
	atVersion(t, "dev")
	if _, err := src.OpenApp("jellyfin", futureEnvelope(t, sec, "example"), now, 0); err == nil {
		t.Error("a dev build read an unknown field")
	} else if _, ok := catalogue.AsNeedsNewer(err); ok {
		t.Errorf("a dev build was told to upgrade: %v", err)
	}
}

func TestIndexEntryCarriesMinVersion(t *testing.T) {
	atVersion(t, "0.7.0")
	k, sec := testKey(t, "example")
	src := Source{Name: "example", Keys: []Key{k}}
	// An entry field this version doesn't know must not hide the index.
	b := []byte(`{"source":"example","serial":3,"expires":"` + now.Add(time.Hour).Format(time.RFC3339) +
		`","apps":[{"id":"jellyfin","serial":5,"name":"Jellyfin","group":"Media","minVersion":"0.8.0","later":1}]}`)
	ix, err := src.OpenIndex(Signed{Data: b, Sig: string(sec.Sign(b))}, now, 0)
	if err != nil || len(ix.Apps) != 1 || ix.Apps[0].MinVersion != "0.8.0" || ix.Apps[0].NeedsNewer() != "0.8.0" {
		t.Fatalf("OpenIndex = %+v, %v", ix, err)
	}
	if (Entry{MinVersion: "0.7.0"}).NeedsNewer() != "" || (Entry{}).NeedsNewer() != "" {
		t.Error("an entry this version can install needs a newer one")
	}
	bad := []byte(`{"source":"example","serial":3,"expires":"` + now.Add(time.Hour).Format(time.RFC3339) +
		`","apps":[{"id":"jellyfin","serial":5,"name":"J","group":"M","minVersion":"soon"}]}`)
	if _, err := src.OpenIndex(Signed{Data: bad, Sig: string(sec.Sign(bad))}, now, 0); err == nil {
		t.Error("a malformed minVersion was accepted")
	}
	if _, err := src.OpenIndex(Signed{Data: b, Sig: string(sec.Sign([]byte("x")))}, now, 0); !errors.Is(err, ErrSignature) {
		t.Errorf("signature must fail first: %v", err)
	}
}

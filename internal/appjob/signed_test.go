package appjob

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/store"
)

const nextDigest = "sha256:5555555555555555555555555555555555555555555555555555555555555555"

// musicBox is Navidrome's definition under another id, as a third-party
// source would publish it, pinned to tag.
func musicBox(t *testing.T, sec store.Secret, serial int64, tag, digest string, expires time.Time) *store.Signed {
	t.Helper()
	b, err := os.ReadFile("../catalogue/apps/navidrome.json")
	if err != nil {
		t.Fatal(err)
	}
	var def map[string]any
	json.Unmarshal(b, &def)
	def["id"], def["name"], def["tag"], def["digest"] = "music-box", "Music Box", tag, digest
	raw, _ := json.Marshal(def)
	env, _, err := store.SealApp("extra", serial, expires, raw)
	if err != nil {
		t.Fatal(err)
	}
	return &store.Signed{Data: env, Sig: string(sec.Sign(env))}
}

func TestSignedApps(t *testing.T) {
	e, rt, media := testEnv(t)
	key, sec, _ := store.GenerateKey("extra", nil)
	_, other, _ := store.GenerateKey("extra", nil)
	e.Sources = []store.Source{{Name: "extra", URL: "https://example.org/", Keys: []store.Key{key}}}
	later := e.Now().Add(24 * time.Hour)
	const firstDigest = "sha256:38dc2727bfcfd5ede290f8ada114fc90368146f265ae4701ddddbcbe2a44ee52"
	v1 := musicBox(t, sec, 1, "0.64.2", firstDigest, later)
	install := func(job string, src string, def *store.Signed) error {
		return e.Run(context.Background(), Request{Job: job, Action: "install", App: "music-box", Source: src, Definition: def,
			Options: catalogue.Options{Media: map[string]string{"music": filepath.Join(media, "music")}}})
	}

	for name, c := range map[string]struct {
		src  string
		def  *store.Signed
		want string
	}{
		"unsigned":      {"extra", nil, "needs its signed definition"},
		"wrong key":     {"extra", musicBox(t, other, 1, "0.64.2", firstDigest, later), store.ErrSignature.Error()},
		"expired":       {"extra", musicBox(t, sec, 1, "0.64.2", firstDigest, e.Now().Add(-time.Hour)), "expired"},
		"untrusted":     {"elsewhere", v1, "isn't a CirrusStore source this server trusts"},
		"not built in":  {"", nil, `no app "music-box"`},
		"other source":  {"extra", tamperSource(t, sec, v1), store.ErrSource.Error()},
		"tampered data": {"extra", &store.Signed{Data: []byte(strings.Replace(string(v1.Data), "Music Box", "Music Bux", 1)), Sig: v1.Sig}, store.ErrSignature.Error()},
	} {
		if err := install("cccc0001", c.src, c.def); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if len(Records(e.RecordsDir)) != 0 || len(rt.calls) != 0 {
		t.Fatal("a refused definition ran")
	}

	if err := install("cccc0002", "extra", v1); err != nil {
		t.Fatal(err)
	}
	rec, _ := e.record("music-box")
	if rec.Source != "extra" || rec.Serial != 1 || rec.Definition == nil || rec.Name != "Music Box" {
		t.Fatalf("record = %+v", rec)
	}

	// A newer definition updates it; the old one can't come back as an
	// "update", but rolling back to the previous version uses it.
	v2 := musicBox(t, sec, 2, "0.65.0", nextDigest, later)
	update := func(job string, def *store.Signed, tag, digest string) error {
		r := Request{Job: job, Action: "update", App: "music-box", Tag: tag, Digest: digest, Definition: def}
		if def != nil {
			r.Source = "extra"
		}
		return e.Run(context.Background(), r)
	}
	if err := update("cccc0003", v2, "0.65.0", nextDigest); err != nil {
		t.Fatal(err)
	}
	if rec, _ = e.record("music-box"); rec.Serial != 2 || rec.PreviousSerial != 1 || rec.Tag != "0.65.0" || rt.calls[len(rt.calls)-1] != "up music-box 0.65.0" {
		t.Fatalf("after the definition update: %+v %v", rec, rt.calls)
	}
	if err := update("cccc0004", v1, "0.64.2", firstDigest); !errors.Is(err, store.ErrReplay) {
		t.Errorf("an older definition: %v", err)
	}
	if err := update("cccc0005", v2, "0.65.0", nextDigest); err == nil || !strings.Contains(err.Error(), "already has that definition") {
		t.Errorf("the same definition again: %v", err)
	}
	if err := update("cccc0006", nil, "0.64.2", firstDigest); err != nil {
		t.Fatal(err)
	}
	if rec, _ = e.record("music-box"); rec.Serial != 1 || rec.Tag != "0.64.2" || rec.PreviousSerial != 2 {
		t.Fatalf("after rolling back: %+v", rec)
	}

	// The source going away doesn't strand the app: removal uses the record.
	e.Sources = nil
	if err := e.Run(context.Background(), Request{Job: "cccc0007", Action: "remove", App: "music-box"}); err != nil {
		t.Fatal(err)
	}
}

// tamperSource re-signs v with the right key but for another source.
func tamperSource(t *testing.T, sec store.Secret, v *store.Signed) *store.Signed {
	t.Helper()
	data := []byte(strings.Replace(string(v.Data), `"source": "extra"`, `"source": "elsewhere"`, 1))
	return &store.Signed{Data: data, Sig: string(sec.Sign(data))}
}

func TestParseRequestSigned(t *testing.T) {
	def := `{"data":"e30=","sig":"x"}`
	for name, body := range map[string]string{
		"definition on remove":   `{"job":"aaaa1111","action":"remove","app":"navidrome","source":"extra","definition":` + def + `}`,
		"definition on built-in": `{"job":"aaaa1111","action":"install","app":"navidrome","source":"built-in","definition":` + def + `}`,
		"definition, no source":  `{"job":"aaaa1111","action":"install","app":"navidrome","definition":` + def + `}`,
		"bad source":             `{"job":"aaaa1111","action":"install","app":"navidrome","source":"../x"}`,
		"bad app id":             `{"job":"aaaa1111","action":"install","app":"../../etc"}`,
	} {
		if _, err := ParseRequest([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseRequest([]byte(`{"job":"aaaa1111","action":"install","app":"music-box","source":"extra","definition":` + def + `}`)); err != nil {
		t.Errorf("a signed install: %v", err)
	}
}

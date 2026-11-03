package store

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func testKey(t *testing.T, name string) (Key, Secret) {
	t.Helper()
	k, s, err := GenerateKey(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return k, s
}

func def(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile("../catalogue/apps/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func seal(t *testing.T, sec Secret, source, id string, serial int64, expires time.Time) Signed {
	t.Helper()
	b, _, err := SealApp(source, serial, expires, def(t, id))
	if err != nil {
		t.Fatal(err)
	}
	return Signed{Data: b, Sig: string(sec.Sign(b))}
}

func TestKeyRoundTrip(t *testing.T) {
	k, s := testKey(t, "example")
	k2, err := ParseKey(k.String())
	if err != nil || !k2.Pub.Equal(k.Pub) || k2.Name != "example" {
		t.Fatalf("ParseKey(%q) = %v, %v", k, k2, err)
	}
	s2, err := ParseSecret(s.String())
	if err != nil || !s2.Public().Pub.Equal(k.Pub) {
		t.Fatalf("ParseSecret: %v", err)
	}
	if !strings.HasPrefix(k.Fingerprint(), "SHA256:") {
		t.Fatalf("fingerprint %q", k.Fingerprint())
	}
	for _, bad := range []string{"", "cirrusstore-key v2 a AAAA", "cirrusstore-key v1 Bad AAAA", "cirrusstore-key v1 ok notbase64!", "ssh-ed25519 AAAA x y"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) accepted", bad)
		}
	}
}

func TestOpenApp(t *testing.T) {
	k, sec := testKey(t, "example")
	_, other := testKey(t, "other")
	src := Source{Name: "example", URL: "https://example.org/", Keys: []Key{k}}
	later := now.Add(30 * 24 * time.Hour)
	good := seal(t, sec, "example", "jellyfin", 5, later)

	a, err := src.OpenApp("jellyfin", good, now, 0)
	if err != nil || a.Def.ID != "jellyfin" || a.Serial != 5 || a.Source != "example" {
		t.Fatalf("OpenApp = %+v, %v", a, err)
	}

	tampered := Signed{Data: []byte(strings.Replace(string(good.Data), `"serial": 5`, `"serial": 9`, 1)), Sig: good.Sig}
	cases := []struct {
		name string
		id   string
		f    Signed
		min  int64
		want error
	}{
		{"wrong key", "jellyfin", seal(t, other, "example", "jellyfin", 5, later), 0, ErrSignature},
		{"tampered", "jellyfin", tampered, 0, ErrSignature},
		{"no signature", "jellyfin", Signed{Data: good.Data}, 0, ErrSignature},
		{"other source", "jellyfin", seal(t, sec, "elsewhere", "jellyfin", 5, later), 0, ErrSource},
		{"expired", "jellyfin", seal(t, sec, "example", "jellyfin", 5, now.Add(-time.Hour)), 0, ErrExpired},
		{"replayed", "jellyfin", good, 6, ErrReplay},
	}
	for _, c := range cases {
		if _, err := src.OpenApp(c.id, c.f, now, c.min); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := src.OpenApp("navidrome", good, now, 0); err == nil {
		t.Error("an envelope for another app id was accepted")
	}
	if _, err := src.OpenApp("jellyfin", good, now, 5); err != nil {
		t.Errorf("the same serial again (a reinstall) was refused: %v", err)
	}
	if _, err := (Source{Name: "example"}).OpenApp("jellyfin", good, now, 0); !errors.Is(err, ErrSignature) {
		t.Errorf("a source with no key verified: %v", err)
	}
}

func TestOpenAppRefusesUnknownFields(t *testing.T) {
	k, sec := testKey(t, "example")
	src := Source{Name: "example", Keys: []Key{k}}
	var app map[string]any
	if err := json.Unmarshal(def(t, "jellyfin"), &app); err != nil {
		t.Fatal(err)
	}
	app["privileged"] = true
	raw, _ := json.Marshal(app)
	env, _ := json.Marshal(map[string]any{"source": "example", "serial": 1, "expires": now.Add(time.Hour), "app": json.RawMessage(raw)})
	if _, err := src.OpenApp("jellyfin", Signed{Data: env, Sig: string(sec.Sign(env))}, now, 0); err == nil {
		t.Fatal("a signed definition with an unknown field was accepted")
	}
	env, _ = json.Marshal(map[string]any{"source": "example", "serial": 1, "expires": now.Add(time.Hour), "app": json.RawMessage(def(t, "jellyfin")), "extra": 1})
	if _, err := src.OpenApp("jellyfin", Signed{Data: env, Sig: string(sec.Sign(env))}, now, 0); err == nil {
		t.Fatal("an envelope with an unknown field was accepted")
	}
}

func TestOpenIndex(t *testing.T) {
	k, sec := testKey(t, "example")
	src := Source{Name: "example", Keys: []Key{k}}
	ix := Index{Source: "example", Serial: 3, Expires: now.Add(time.Hour), Apps: []Entry{{ID: "jellyfin", Serial: 5, Name: "Jellyfin", Group: "Media"}}}
	b, _ := json.Marshal(ix)
	got, err := src.OpenIndex(Signed{Data: b, Sig: string(sec.Sign(b))}, now, 3)
	if err != nil || len(got.Apps) != 1 {
		t.Fatalf("OpenIndex = %+v, %v", got, err)
	}
	if _, err := src.OpenIndex(Signed{Data: b, Sig: string(sec.Sign(b))}, now, 4); !errors.Is(err, ErrReplay) {
		t.Errorf("an older index: %v", err)
	}
	ix.Apps = append(ix.Apps, ix.Apps[0])
	b, _ = json.Marshal(ix)
	if _, err := src.OpenIndex(Signed{Data: b, Sig: string(sec.Sign(b))}, now, 0); err == nil {
		t.Error("an index listing an id twice was accepted")
	}
}

func TestParseSource(t *testing.T) {
	k, _ := testKey(t, "example")
	s, err := ParseSource("example", []byte("# by setup\nurl=https://example.org/apps\nkey="+k.String()+"\n"))
	if err != nil || s.URL != "https://example.org/apps/" || len(s.Keys) != 1 {
		t.Fatalf("ParseSource = %+v, %v", s, err)
	}
	for name, body := range map[string]string{
		"example":     "url=http://example.org/\nkey=" + k.String(),
		"example-2":   "url=https://example.org/",
		"example-3":   "url=https://u:p@example.org/\nkey=" + k.String(),
		"example-4":   "url=https://example.org/\nkey=" + k.String() + "\nother=1",
		Official:      "url=https://example.org/\nkey=" + k.String(),
		"Not-A-Name":  "url=https://example.org/\nkey=" + k.String(),
		"example-5":   "url=https://example.org/?q=1\nkey=" + k.String(),
		"example-six": "key=" + k.String(),
	} {
		if _, err := ParseSource(name, []byte(body)); err == nil {
			t.Errorf("ParseSource(%s, %q) accepted", name, body)
		}
	}
	back, err := FromInfo(s.Info())
	if err != nil || back.URL != s.URL || len(back.Keys) != 1 || !back.Keys[0].Pub.Equal(k.Pub) {
		t.Fatalf("FromInfo(Info()) = %+v, %v", back, err)
	}
}

func TestOfficialSource(t *testing.T) {
	s := OfficialSource()
	if s.Name != Official || !s.Official || s.URL != OfficialURL {
		t.Fatalf("OfficialSource = %+v", s)
	}
	for _, k := range s.Keys {
		if k.Fingerprint() == "" {
			t.Fatal("official key without fingerprint")
		}
	}
}

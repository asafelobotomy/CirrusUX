package appjob

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"cirrusux/internal/catalogue"
	"cirrusux/internal/store"
	"cirrusux/internal/version"
)

// A definition for a newer CirrusUX gets the plain refusal, not a parse
// error, and nothing runs; a bad signature still fails first.
func TestHelperRefusesDefinitionNeedingNewerCirrusUX(t *testing.T) {
	prev := version.Version
	version.Version = "0.7.0"
	defer func() { version.Version = prev }()

	e, rt, media := testEnv(t)
	key, sec, _ := store.GenerateKey("extra", nil)
	_, other, _ := store.GenerateKey("extra", nil)
	e.Sources = []store.Source{{Name: "extra", URL: "https://example.org/", Keys: []store.Key{key}}}
	later := e.Now().Add(24 * time.Hour)

	b, _ := os.ReadFile("../catalogue/apps/navidrome.json")
	var def map[string]any
	json.Unmarshal(b, &def)
	def["id"], def["name"], def["minVersion"], def["fromTheFuture"] = "music-box", "Music Box", "0.8.0", true
	raw, _ := json.Marshal(def)
	env, _ := json.Marshal(map[string]any{"source": "extra", "serial": 1, "expires": later, "app": json.RawMessage(raw)})
	sign := func(s store.Secret) *store.Signed { return &store.Signed{Data: env, Sig: string(s.Sign(env))} }
	run := func(def *store.Signed) error {
		return e.Run(context.Background(), Request{Job: "dddd0001", Action: "install", App: "music-box", Source: "extra", Definition: def,
			Options: catalogue.Options{Media: map[string]string{"music": media}}})
	}

	err := run(sign(sec))
	if need, ok := catalogue.AsNeedsNewer(err); !ok || need.Min != "0.8.0" {
		t.Fatalf("got %v, want ErrNeedsNewer 0.8.0", err)
	}
	if err := run(sign(other)); !errors.Is(err, store.ErrSignature) {
		t.Errorf("bad signature: %v", err)
	}
	if len(Records(e.RecordsDir)) != 0 || len(rt.calls) != 0 {
		t.Fatal("a refused definition ran")
	}
}

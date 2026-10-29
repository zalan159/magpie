package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCteno(t *testing.T) {
	home, _ := museHome(t)
	dir := filepath.Join(home, "cteno")
	t.Setenv("CTENO_APP_DATA_DIR", dir)
	path := filepath.Join(dir, "profiles.json")
	os.MkdirAll(dir, 0o755)
	// a user's own: a profile of theirs as the default, and a key of Cteno's
	orig := `{
  "profiles": [{"id": "mine", "name": "Mine", "api_format": "openai", "chat": {"api_key": "secret://vault/k", "base_url": "https://api.example.com/v1", "model": "m", "temperature": 0.7, "max_tokens": 8192, "context_window_tokens": null}, "compress": {"api_key": "secret://vault/k", "base_url": "https://api.example.com/v1", "model": "m", "temperature": 0.3, "max_tokens": 8192, "context_window_tokens": null}}],
  "default_profile_id": "mine",
  "subscription_compress": {}
}
`
	os.WriteFile(path, []byte(orig), 0o644)
	read := func() map[string]any {
		t.Helper()
		var m map[string]any
		b, _ := os.ReadFile(path)
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return m
	}
	var want map[string]any
	json.Unmarshal([]byte(orig), &want)

	a := cteno(home)
	f := a.Field("model")
	if a.Path != path || !a.Detected() || f.Get() != "mine" || a.Check() != "" {
		t.Fatalf("own: %s %q %q", a.Path, f.Get(), a.Check())
	}
	for range 2 { // twice: one magpie profile, and the user's default kept
		if err := f.Set("magpie/deepseek/pro"); err != nil {
			t.Fatal(err)
		}
	}
	m := read()
	ps := m["profiles"].([]any)
	if len(ps) != 2 || !reflect.DeepEqual(ps[0], want["profiles"].([]any)[0]) || m["default_profile_id"] != "magpie" {
		t.Fatalf("connected: %v", m)
	}
	p := ps[1].(map[string]any)
	for _, end := range []string{"chat", "compress"} {
		e := p[end].(map[string]any)
		if e["base_url"] != gatewayV1() || e["api_key"] != "magpie-cteno" || e["model"] != "deepseek/pro" {
			t.Fatalf("%s: %v", end, e)
		}
	}
	if p["picker_group"] != "Magpie" || p["api_format"] != "anthropic" || f.Get() != "magpie/deepseek/pro" || a.Check() != "" {
		t.Fatalf("magpie's: %v %q %q", p, f.Get(), a.Check())
	}

	// Cteno wrote its own copy over it: told
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(orig, `"default_profile_id": "mine"`, `"default_profile_id": "magpie"`, 1)), 0o644)
	if !strings.Contains(a.Check(), "gone") {
		t.Fatalf("check: %q", a.Check())
	}
	os.WriteFile(path, b, 0o644)

	// out: as it was
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if m := read(); !reflect.DeepEqual(m, want) {
		t.Fatalf("back: %v", m)
	}

	// a file magpie made goes again
	os.Remove(path)
	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if isFile(path) {
		t.Fatal("magpie's file left behind")
	}
}

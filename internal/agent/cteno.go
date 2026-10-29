package agent

// Cteno (cteno.cn) keeps its models in profiles.json in its app-data folder
// (com.cteno-host.gui: ~/Library/Application Support on a Mac, %APPDATA% on
// Windows, $XDG_DATA_HOME or ~/.local/share elsewhere; CTENO_APP_DATA_DIR
// names another): a "profiles" array of {id, name, api_format, chat,
// compress, picker_group, …}, chat and compress each {api_key, base_url,
// model, temperature, max_tokens, context_window_tokens}, and the one new
// sessions start on in default_profile_id. Cteno uses a base_url as it is.
//
// magpie adds a profile of its own, id "magpie", in a "Magpie" section of
// Cteno's model picker, on Anthropic Messages at the gateway, and points
// default_profile_id at it; the default the user had is stashed and put
// back when magpie steps out, and the "magpie" profile goes again then. The
// user's profiles and other keys are left as they are.

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/edit"
)

const (
	ctenoProfiles = "profiles"
	ctenoDefault  = "default_profile_id"
)

func cteno(home string) *Agent {
	dir := appdir.Getenv("CTENO_APP_DATA_DIR")
	if dir == "" {
		const id = "com.cteno-host.gui"
		switch {
		case runtime.GOOS == "darwin":
			dir = filepath.Join(home, "Library", "Application Support", id)
		case runtime.GOOS == "windows" && appdir.Getenv("APPDATA") != "":
			dir = filepath.Join(appdir.Getenv("APPDATA"), id)
		case appdir.Getenv("XDG_DATA_HOME") != "":
			dir = filepath.Join(appdir.Getenv("XDG_DATA_HOME"), id)
		default:
			dir = filepath.Join(home, ".local", "share", id)
		}
	}
	return ctenoAt(here(home), dir)
}

func ctenoAt(at place, dir string) *Agent {
	path := filepath.Join(dir, "profiles.json")
	keyDefault := "cteno:" + path + ":" + ctenoDefault
	keyNew := "cteno:" + path + ":new"
	def := func() string { v, _ := edit.GetJSON(path, ctenoDefault); return v }
	// ours is magpie's profile, "" where there is none
	ours := func() string {
		raw, err := edit.Read(path)
		if err != nil {
			return ""
		}
		for _, p := range gjson.GetBytes(raw, ctenoProfiles).Array() {
			if p.Get("id").String() == magpieID {
				return p.Raw
			}
		}
		return ""
	}
	// model is magpie/<catalog id> while Cteno starts on magpie's profile,
	// else the profile id it starts on
	model := func() string {
		if d := def(); d != magpieID {
			return d
		}
		return magpieID + "/" + gjson.Get(ours(), "chat.model").String()
	}
	on := func() bool { return def() == magpieID && isMagpie(gjson.Get(ours(), "chat.model").String()) }
	// profiles sets the profiles array to the user's ones plus, for a
	// model ref, magpie's on it
	profiles := func(ref string) error {
		raw, err := edit.Read(path)
		if err != nil && isFile(path) {
			return err
		}
		list := []json.RawMessage{}
		for _, p := range gjson.GetBytes(raw, ctenoProfiles).Array() {
			if p.Get("id").String() != magpieID {
				list = append(list, json.RawMessage(p.Raw))
			}
		}
		if ref != "" {
			end := func(temp float64) map[string]any {
				return map[string]any{"api_key": agentKeyAt("cteno", at.gw()), "base_url": at.v1(), "model": ref,
					"temperature": temp, "max_tokens": 8192, "context_window_tokens": nil}
			}
			mine, _ := json.Marshal(map[string]any{
				"id": magpieID, "name": ref, "api_format": "anthropic", "picker_group": "Magpie",
				"chat": end(0.7), "compress": end(0.3),
			})
			list = append(list, mine)
		}
		return edit.SetJSON(path, edit.KV{Path: ctenoProfiles, Value: list})
	}
	put := func(v string) error {
		if v == "" {
			if !isFile(path) {
				return nil
			}
			return edit.DelJSON(path, ctenoDefault)
		}
		return edit.SetJSON(path, edit.KV{Path: ctenoDefault, Value: v})
	}
	return atomic(&Agent{
		ID: "cteno", Name: "Cteno", Icon: "generic", Dir: dir, Path: path,
		Notice: func() string {
			if Running(`cteno-host-cli serve`) {
				return noticeCteno.String()
			}
			return ""
		},
		Check: func() string {
			if def() != magpieID {
				return ""
			}
			raw := ours()
			if raw == "" {
				return "Cteno's magpie profile (profiles.json) is gone, so it no longer reaches magpie"
			}
			return wiringOff("Cteno", path, func(k string) (string, bool) {
				r := gjson.Get(raw, k)
				return r.String(), r.Exists()
			}, "chat.base_url", at.v1())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: model,
			Set: func(v string) error {
				if ref, ok := strings.CutPrefix(v, magpieID+"/"); ok && isMagpie(ref) {
					// what the user had, unless magpie stashed it already
					if !on() {
						forget(keyDefault, keyNew)
						stash(map[string]string{keyDefault: def()})
						if !isFile(path) {
							stash(map[string]string{keyNew: "1"})
						}
					}
					if err := profiles(ref); err != nil {
						return err
					}
					return put(magpieID)
				}
				made := false
				if def() == magpieID || ours() != "" {
					made = unstash(keyNew) != ""
					// back to what the user had
					if v == "" {
						v = unstash(keyDefault)
					} else {
						forget(keyDefault)
					}
					if err := profiles(""); err != nil {
						return err
					}
				}
				if err := put(v); err != nil {
					return err
				}
				if made {
					if raw, err := edit.Read(path); err == nil && len(gjson.GetBytes(raw, ctenoProfiles).Array()) == 0 {
						return edit.Remove(path)
					}
				}
				return nil
			},
			Options: func(cur map[string]string) []Option {
				var own []Option
				if c := cur["model"]; c != "" && !usesMagpie(c) {
					own = append(own, Option{Value: c, Icon: modelIcon("", c)})
				}
				return append(group("Cteno", own), viaMagpie("cteno", magpieID+"/")...)
			},
		}},
	}, path)
}

// what cteno says after a change (notice.go)
var (
	noticeCteno = newNotice("Cteno reads its model profiles at start-up — restart Cteno to use this.")
)

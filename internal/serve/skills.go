package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"gophermind/internal/skills"
)

// SkillsDeps configures the skill routes. Root is the project whose
// .gophermind/skills packs are always on; ConfigDir holds the settings file
// and the fetched cache.
type SkillsDeps struct {
	Root      string
	ConfigDir string
}

// skillsSettingsPath is where enablement and sources are stored.
func skillsSettingsPath(configDir string) string {
	return configDir + "/skills.json"
}

// skillsListHandler handles GET /skills: the catalogue, with each skill's
// source and whether it is switched on.
//
// It reports a settings read failure rather than answering with defaults. The
// safe default here is "nothing enabled", and quietly returning that would
// render a panel showing every skill off when the user had turned some on,
// which invites them to turn things on twice.
func skillsListHandler(d SkillsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		s, err := skills.LoadSettings(skillsSettingsPath(d.ConfigDir))
		if err != nil {
			http.Error(w, "read skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		out := struct {
			Sources []skills.Source `json:"sources"`
			Skills  []skills.Skill  `json:"skills"`
		}{
			Sources: s.Sources,
			Skills:  skills.Catalogue(d.Root, s, skills.CacheDir(d.ConfigDir)),
		}
		if out.Sources == nil {
			out.Sources = []skills.Source{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// skillsPatchHandler handles PATCH /skills: switch one skill on or off by its
// scoped key.
//
// A repo-local pack cannot be toggled. It was committed to the project
// deliberately, so its consent is the commit, and pretending otherwise would
// put a control in the UI that does nothing on the next run.
func skillsPatchHandler(d SkillsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "use PATCH", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Key     string `json:"key"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Key) == "" || body.Enabled == nil {
			http.Error(w, "key and enabled are required", http.StatusBadRequest)
			return
		}
		if !strings.Contains(body.Key, ":") {
			http.Error(w, "repo-local skills are always on and cannot be toggled",
				http.StatusBadRequest)
			return
		}

		path := skillsSettingsPath(d.ConfigDir)
		s, err := skills.LoadSettings(path)
		if err != nil {
			http.Error(w, "read skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.Enabled = setEnabled(s.Enabled, body.Key, *body.Enabled)
		if err := skills.SaveSettings(path, s); err != nil {
			http.Error(w, "save skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(skills.Catalogue(d.Root, s, skills.CacheDir(d.ConfigDir)))
	}
}

// setEnabled adds or removes a key, keeping the list free of duplicates.
func setEnabled(list []string, key string, on bool) []string {
	out := make([]string, 0, len(list)+1)
	for _, k := range list {
		if k != key {
			out = append(out, k)
		}
	}
	if on {
		out = append(out, key)
	}
	return out
}

// skillsSourceAddHandler handles POST /skills/sources: clone a repository at a
// pinned commit and record it.
//
// Nothing from the new source is enabled. That is the whole point: the content
// is now reviewable on disk, and switching any of it on is a second, separate
// decision. Cloning is also why this route is not a GET; it reaches the
// network and writes to disk.
func skillsSourceAddHandler(d SkillsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			URL string `json:"url"`
			Ref string `json:"ref"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := skills.ValidateSourceURL(body.URL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		path := skillsSettingsPath(d.ConfigDir)
		s, err := skills.LoadSettings(path)
		if err != nil {
			http.Error(w, "read skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id := skills.SourceID(body.URL)
		for _, existing := range s.Sources {
			if skills.SourceID(existing.URL) == id {
				http.Error(w, "source already added: "+id, http.StatusConflict)
				return
			}
		}

		src, err := skills.Fetch(r.Context(), skills.CacheDir(d.ConfigDir), body.URL, body.Ref)
		if err != nil {
			http.Error(w, "fetch: "+err.Error(), http.StatusBadGateway)
			return
		}
		s.Sources = append(s.Sources, src)
		if err := skills.SaveSettings(path, s); err != nil {
			http.Error(w, "save skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Source skills.Source  `json:"source"`
			Skills []skills.Skill `json:"skills"`
		}{src, skills.Catalogue(d.Root, s, skills.CacheDir(d.ConfigDir))})
	}
}

// skillsSourceDeleteHandler handles DELETE /skills/sources/{id...}: remove a
// repository, its cached content, and every enablement that referred to it.
func skillsSourceDeleteHandler(d SkillsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "use DELETE", http.StatusMethodNotAllowed)
			return
		}
		id := strings.Trim(r.PathValue("id"), "/")
		if id == "" || strings.Contains(id, "..") {
			http.Error(w, "bad source id", http.StatusBadRequest)
			return
		}
		path := skillsSettingsPath(d.ConfigDir)
		s, err := skills.LoadSettings(path)
		if err != nil {
			http.Error(w, "read skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s, err = skills.Remove(skills.CacheDir(d.ConfigDir), s, id)
		if err != nil {
			http.Error(w, "remove: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := skills.SaveSettings(path, s); err != nil {
			http.Error(w, "save skill settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

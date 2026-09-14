package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gophermind/gophermind-lib/freellm"
	"gophermind/gophermind-lib/modelcat"
)

// catalogueHandler handles GET /models/catalogue: it returns every model
// gophermind knows about, with reachability, remaining allowance and links.
// endpointModels, when non-nil, supplies the ids the active configured
// endpoint serves; nil omits those entries from the catalogue.
func catalogueHandler(endpointModels func() []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		o, _ := freellm.LoadOdometer(freellm.OdometerPath())
		// A settings read failure is reported rather than absorbed: the
		// catalogue is filtered by the user's exclusions, so rendering it
		// from settings that are not theirs would show, as selectable,
		// providers they excluded for legal reasons.
		s, err := modelcat.LoadSettings(modelcat.SettingsPath())
		if err != nil {
			http.Error(w, fmt.Sprintf("could not read your model settings: %v", err), http.StatusInternalServerError)
			return
		}
		var models []string
		if endpointModels != nil {
			models = endpointModels()
		}
		entries := modelcat.Build(o, s, models, time.Now())
		if entries == nil {
			entries = []modelcat.Entry{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string][]modelcat.Entry{"entries": entries})
	}
}

// settingsGetHandler handles GET /models/settings: it returns the stored
// model picker settings, or DefaultSettings when none have been saved yet.
func settingsGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		s, err := modelcat.LoadSettings(modelcat.SettingsPath())
		if err != nil {
			http.Error(w, fmt.Sprintf("could not read your model settings: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s)
	}
}

// settingsPatchHandler handles PATCH /models/settings: it merges a partial
// JSON object over the stored settings and persists the result. Any custom
// link that fails modelcat.ValidateLink is rejected with 400, naming the
// offending scheme, and nothing is stored: this is the security boundary
// that keeps a javascript: or file: URL out of the settings a WebView later
// renders as a clickable link.
func settingsPatchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "use PATCH", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var linkCheck struct {
			CustomLinks map[string]string `json:"custom_links"`
		}
		if err := json.Unmarshal(body, &linkCheck); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		for key, link := range linkCheck.CustomLinks {
			if err := modelcat.ValidateLink(link); err != nil {
				http.Error(w, fmt.Sprintf("invalid custom link for %q: %v", key, err), http.StatusBadRequest)
				return
			}
		}
		// The stored settings are this patch's merge base, so a failed read
		// would persist a base that is not the user's, permanently dropping
		// preferences and exclusions the file still holds. Refuse instead.
		current, err := modelcat.LoadSettings(modelcat.SettingsPath())
		if err != nil {
			http.Error(w, fmt.Sprintf("could not read your model settings, so nothing was changed: %v", err), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(body, &current); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := modelcat.SaveSettings(modelcat.SettingsPath(), current); err != nil {
			http.Error(w, "save settings", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(current)
	}
}

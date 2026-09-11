package freellm

import (
	"sort"
	"strings"
)

// ProfilePrefix is the gophermind profile-name prefix reserved for free
// providers, so they never collide with the built-in profiles.
const ProfilePrefix = "free-"

// Terms records free-tier obligations worth warning a user about before they
// point gophermind at someone else's code.
type Terms struct {
	NonCommercial   bool // free tier forbids commercial use
	TrainsOnPrompts bool // provider may train on free-tier prompts
	IdentityCheck   bool // signup requires real-name or account verification
}

// TermsFlags returns the free-tier obligations as short labels, so each
// display surface can join them its own way without reimplementing the
// field-to-label mapping.
func TermsFlags(t Terms) []string {
	var f []string
	if t.NonCommercial {
		f = append(f, "non-commercial")
	}
	if t.TrainsOnPrompts {
		f = append(f, "trains on prompts")
	}
	if t.IdentityCheck {
		f = append(f, "identity check")
	}
	return f
}

// Compat is what gophermind knows about running against a free provider, as
// distinct from what upstream records about the provider itself. This is the
// only hand-maintained data in the package; data.json is never edited.
type Compat struct {
	Profile      string // gophermind profile name, e.g. "free-groq"
	Upstream     string // provider "name" in data.json; must resolve
	BaseURL      string // OpenAI-compatible endpoint; may differ from upstream baseUrl
	DefaultModel string // explicit; free profiles never auto-discover
	Website      string // human-facing site, shown in attribution
	Affiliate    string // referral URL; empty for every provider today
	NoKey        bool   // serves requests anonymously
	Supported    bool   // false => listed for discovery, not runnable as a profile
	Terms        Terms
	Note         string // why this entry overrides upstream, or is unsupported

	// ChatPath is llm.Client.ChatPath for this provider. Empty means the
	// client's default "/v1/chat/completions". Set it when BaseURL already
	// ends in the API version segment (true of every entry here except
	// free-kilocode), so the client does not double it into
	// /v1/v1/chat/completions.
	ChatPath string

	// ModelsPath is llm.Client.ModelsPath for this provider, mirroring
	// ChatPath exactly: empty means the client's default "/v1/models"; set it
	// alongside ChatPath for the same entries, since a BaseURL that already
	// ends in the API version segment doubles /v1/models the same way it
	// doubles /v1/chat/completions.
	ModelsPath string
}

// compats is the table. Ordering here is irrelevant; Compats sorts.
var compats = []Compat{
	{
		Profile: "free-kilocode", Upstream: "Kilo Code",
		BaseURL:      "https://api.kilo.ai/api/gateway",
		DefaultModel: "nvidia/nemotron-3-super-120b-a12b:free",
		Website:      "https://kilo.ai", NoKey: true, Supported: true,
	},
	{
		Profile: "free-llm7", Upstream: "LLM7.io",
		BaseURL: "https://api.llm7.io/v1",
		Website: "https://llm7.io", Supported: false,
		Note: "upstream's DefaultModel (gpt-oss:20b) is not in the 45 models the live endpoint " +
			"advertises, and no advertised model reaches gophermind's tool-calling agent loop " +
			"anonymously: of 8 candidates tried against the live endpoint on 2026-09-10, " +
			"L3-8B-Lunaris-v1-Turbo and mistral-Small-24B-Instruct-2501 both returned HTTP 400 " +
			"(\"does not support tools\", code unsupported_model_feature), and the other 6 " +
			"(deepseek-v4-flash:0731, glm-5.3, kimi-k3, llama-4-maverick, gpt-5.5, gemma4:31b) " +
			"returned HTTP 401 (\"Missing API key\"). Supply your own key and model via env " +
			"(see the _MODEL and key env vars for this profile) to use it with a free token " +
			"from token.llm7.io.",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-ovhcloud", Upstream: "OVHcloud AI Endpoints",
		BaseURL:      "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1",
		DefaultModel: "gpt-oss-120b",
		Website:      "https://endpoints.ai.cloud.ovh.net", NoKey: true, Supported: true,
		Note:       "anonymous tier is 2 requests per minute per IP per model, hosted in the EU",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-aionlabs", Upstream: "Aion Labs",
		BaseURL:      "https://api.aionlabs.ai/v1",
		DefaultModel: "aion-labs/aion-3.0",
		Website:      "https://www.aionlabs.ai", Supported: true,
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-cloudflare", Upstream: "Cloudflare Workers AI",
		Website: "https://developers.cloudflare.com/workers-ai/", Supported: false,
		// This entry is never runnable without a _BASE_URL override (see
		// Supported: false above), and config.ApplyProfile no longer lets a
		// base-URL override inherit this table's ChatPath/ModelsPath (a
		// hand-supplied endpoint is not guaranteed to share this entry's path
		// shape). So a ChatPath/ModelsPath set here would never be read; the
		// Note tells the user to set _CHAT_PATH/_MODELS_PATH themselves
		// instead of carrying dead defaults.
		Note: "endpoint embeds an account ID and cannot be known statically; set GOPHERMIND_PROFILE_FREE_CLOUDFLARE_BASE_URL to your own endpoint. A base-URL override here always requires setting GOPHERMIND_PROFILE_FREE_CLOUDFLARE_CHAT_PATH and _MODELS_PATH by hand too: use /chat/completions and /models if your URL already ends in /v1, or /v1/chat/completions and /v1/models if it does not",
	},
	{
		Profile: "free-cohere", Upstream: "Cohere",
		BaseURL:      "https://api.cohere.ai/compatibility/v1",
		DefaultModel: "command-a-03-2025",
		Website:      "https://cohere.com", Supported: true,
		Terms:      Terms{NonCommercial: true},
		Note:       "upstream records the native /v2 API; this is Cohere's OpenAI compatibility endpoint",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-gemini", Upstream: "Google Gemini",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
		DefaultModel: "gemini-3.5-flash",
		Website:      "https://ai.google.dev", Supported: true,
		Terms:      Terms{TrainsOnPrompts: true},
		Note:       "upstream records the native v1beta API; this is Google's OpenAI compatibility endpoint",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-groq", Upstream: "Groq",
		BaseURL:      "https://api.groq.com/openai/v1",
		DefaultModel: "openai/gpt-oss-120b",
		Website:      "https://groq.com", Supported: true,
		Note:       "the /openai/v1 path is correct and matches upstream; it is not a typo",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-huggingface", Upstream: "Hugging Face",
		BaseURL:      "https://router.huggingface.co/v1",
		DefaultModel: "Qwen/Qwen2.5-7B-Instruct",
		Website:      "https://huggingface.co", Supported: true,
		Note:       "free tier is a monthly credit balance, not a token or request quota",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-mistral", Upstream: "Mistral AI",
		BaseURL:      "https://api.mistral.ai/v1",
		DefaultModel: "mistral-small-2603",
		Website:      "https://mistral.ai", Supported: true,
		Terms:      Terms{TrainsOnPrompts: true},
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-modelscope", Upstream: "ModelScope",
		BaseURL:      "https://api-inference.modelscope.cn/v1",
		DefaultModel: "Qwen/Qwen3.5-35B-A3B",
		Website:      "https://modelscope.cn", Supported: true,
		Terms:      Terms{IdentityCheck: true},
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-nvidia", Upstream: "NVIDIA NIM",
		BaseURL:      "https://integrate.api.nvidia.com/v1",
		DefaultModel: "openai/gpt-oss-120b",
		Website:      "https://build.nvidia.com", Supported: true,
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-ollama-cloud", Upstream: "Ollama Cloud",
		BaseURL:      "https://ollama.com/v1",
		DefaultModel: "gpt-oss:120b",
		Website:      "https://ollama.com", Supported: true,
		Note:       "upstream records the native /api endpoint; this is Ollama's OpenAI compatibility endpoint",
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-openrouter", Upstream: "OpenRouter",
		BaseURL:      "https://openrouter.ai/api/v1",
		DefaultModel: "nvidia/nemotron-3-super-120b-a12b:free",
		Website:      "https://openrouter.ai", Supported: true,
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-siliconflow", Upstream: "SiliconFlow",
		BaseURL:      "https://api.siliconflow.cn/v1",
		DefaultModel: "Qwen/Qwen3-8B",
		Website:      "https://siliconflow.cn", Supported: true,
		Terms:      Terms{IdentityCheck: true},
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
	{
		Profile: "free-zai", Upstream: "Z AI (Zhipu AI)",
		BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		DefaultModel: "glm-4.7-flash",
		Website:      "https://z.ai", Supported: true,
		ChatPath:   "/chat/completions",
		ModelsPath: "/models",
	},
}

// Compats returns the compatibility table sorted for display: providers that
// need no API key first (the zero-signup path), then supported providers, then
// unsupported ones, each group alphabetical by profile name.
func Compats() []Compat {
	out := make([]Compat, len(compats))
	copy(out, compats)
	rank := func(c Compat) int {
		switch {
		case c.NoKey && c.Supported:
			return 0
		case c.Supported:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return out[i].Profile < out[j].Profile
	})
	return out
}

// CompatFor returns the entry for a gophermind profile name.
func CompatFor(profile string) (Compat, bool) {
	for _, c := range compats {
		if c.Profile == profile {
			return c, true
		}
	}
	return Compat{}, false
}

// CompatForBaseURL returns the entry whose endpoint matches baseURL, so a
// caller holding only a configured client can tell which free provider (if
// any) that client is pointed at.
//
// A miss is the normal case, not an error: it means the endpoint is the
// user's own (a local llama.cpp or LM Studio, a private vLLM), which is
// exactly the distinction callers need, because a model id from one
// provider is meaningless at another's endpoint.
//
// Matching ignores a trailing slash so a BaseURL that has been normalised
// one way or the other still resolves.
func CompatForBaseURL(baseURL string) (Compat, bool) {
	want := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if want == "" {
		return Compat{}, false
	}
	for _, c := range compats {
		if strings.TrimRight(c.BaseURL, "/") == want {
			return c, true
		}
	}
	return Compat{}, false
}

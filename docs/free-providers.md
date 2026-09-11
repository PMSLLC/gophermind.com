# Free LLM providers

Generated from `internal/freellm/data.json` and `internal/freellm/compat.go`.
Do not edit by hand; run `scripts/sync-free-providers.sh`.

Registry vendored from [mnfst/awesome-free-llm-apis](https://github.com/mnfst/awesome-free-llm-apis)
(CC0 1.0), upstream `lastUpdated`: 2026-08-21.

Run one with `gophermind --profile <profile> ask "hello"`, or browse them with
`gophermind free list`.

| Provider | Models | Free tier | Link |
|---|---|---|---|
| Aion Labs | 4 | Permanent free tier, no credit card required. 15 RPM, 20K tokens/day. Specialized for roleplay and storytelling. | https://www.aionlabs.ai/app/api-keys/ |
| Cohere | 11 | Free "Trial" API key, no credit card. 1,000 API calls/month. Non-commercial use only. | https://dashboard.cohere.com/api-keys |
| Google Gemini | 10 | Free tier, no credit card. Free-tier prompts may be used by Google to improve products. | https://aistudio.google.com/app/apikey |
| Mistral AI | 7 | Free mode, enabled by default, no credit card required. $10/month in API credits, and free-mode prompts may be used to train Mistral models unless you opt out. | https://console.mistral.ai/api-keys |
| Z AI (Zhipu AI) | 3 | Permanent free models, no credit card required. | https://open.bigmodel.cn/usercenter/apikeys |
| Cloudflare Workers AI | 8 | 10,000 Neurons/day free, no credit card required. 75+ models available on the free tier. | https://dash.cloudflare.com/profile/api-tokens |
| Groq | 5 | Free tier, no credit card. Ultra-fast LPU inference. | https://console.groq.com/keys |
| Hugging Face | 6 | $0.10/month in Inference Provider credits for free users (subject to change). Routes to Fireworks, Together, Hyperbolic, Nebius, Novita, DeepInfra and others. Thousands of models. | https://huggingface.co/settings/tokens |
| Kilo Code | 11 | Free models with no credit card and no API key required. `kilo-auto/free` auto-router dynamically routes to models in the free pool. | https://app.kilo.ai/profile |
| LLM7.io | 3 | API gateway with a free tier. Anonymous access needs no key and reaches the `turbo` models; a free token from token.llm7.io raises the rate and token limits but reaches the same models. | https://token.llm7.io |
| ModelScope | 3 | Free API-Inference for registered users. Requires Alibaba Cloud account binding + real-name verification. | https://modelscope.cn/my/myaccesstoken |
| NVIDIA NIM | 12 | Free with NVIDIA Developer Program membership. 100+ models. Rate-limited per model. | https://build.nvidia.com/explore/discover |
| Ollama Cloud | 10 | Free tier with usage limits. 16 cloud model families from the Ollama library. OpenAI SDK-compatible via https://ollama.com/v1. | https://ollama.com/settings/keys |
| OpenRouter | 12 | 17 free models (marked with `:free` suffix). OpenAI SDK-compatible. | https://openrouter.ai/keys |
| OVHcloud AI Endpoints | 12 | Free anonymous tier (no API key, no signup): 2 RPM per IP per model. 20+ open-weight models hosted in EU. OpenAI SDK-compatible. | https://www.ovhcloud.com/en/public-cloud/ai-endpoints/catalog/ |
| SiliconFlow | 1 | Permanently free models, no credit card required. Identity verification required. 100+ models in the catalog, most of them paid. | https://cloud.siliconflow.cn/account/ak |

## Providers that need no API key

These serve requests anonymously, which makes them the zero-signup way to try
gophermind. Each was verified against a live endpoint when this shipped;
re-verify with `gophermind free check <profile>`.

- `free-kilocode` - Kilo Code (200 req/hr)
- `free-ovhcloud` - OVHcloud AI Endpoints (anonymous tier is 2 requests per minute per IP per model, hosted in the EU)

## Profiles listed for discovery but not runnable

`gophermind free list` shows these too, so `gophermind free show <profile>`
always resolves, but `gophermind --profile <profile>` refuses to start until
the noted blocker is addressed.

- `free-cloudflare` - Cloudflare Workers AI: endpoint embeds an account ID and cannot be known statically; set GOPHERMIND_PROFILE_FREE_CLOUDFLARE_BASE_URL to your own endpoint. A base-URL override here always requires setting GOPHERMIND_PROFILE_FREE_CLOUDFLARE_CHAT_PATH and _MODELS_PATH by hand too: use /chat/completions and /models if your URL already ends in /v1, or /v1/chat/completions and /v1/models if it does not
- `free-llm7` - LLM7.io: upstream's DefaultModel (gpt-oss:20b) is not in the 45 models the live endpoint advertises, and no advertised model reaches gophermind's tool-calling agent loop anonymously: of 8 candidates tried against the live endpoint on 2026-09-10, L3-8B-Lunaris-v1-Turbo and mistral-Small-24B-Instruct-2501 both returned HTTP 400 ("does not support tools", code unsupported_model_feature), and the other 6 (deepseek-v4-flash:0731, glm-5.3, kimi-k3, llama-4-maverick, gpt-5.5, gemma4:31b) returned HTTP 401 ("Missing API key"). Supply your own key and model via env (see the _MODEL and key env vars for this profile) to use it with a free token from token.llm7.io.

## Free-tier terms worth reading

Some free tiers carry obligations beyond a rate limit. `gophermind free show
<profile>` prints them in full, and `gophermind free list` flags them.

- non-commercial: Cohere
- trains on prompts: Google Gemini, Mistral AI
- identity check: ModelScope, SiliconFlow

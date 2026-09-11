# Affiliate and referral links: policy and plan

**Status as of 2026-09-10: gophermind ships no affiliate links.** Every
`Affiliate` field in `internal/freellm/compat.go` is empty.

## Why there are none yet

A survey on 2026-09-10 found no public affiliate or referral program for any of
the providers in the registry. OpenRouter's referral arrangement is not
public. The rest are free tiers with no program at all.

`free-llm7` (LLM7.io) was dropped from consideration entirely, and from the
registry's runnable profiles: as of 2026-09-10 it is `Supported: false` in
`compat.go`. Live probing of 8 candidate models on its anonymous tier found
that every tool-capable model returns HTTP 401 (no key), while the models
that do answer anonymously reject tool calls outright (HTTP 400
`unsupported_model_feature`). Separately, LLM7's advertised model names
(`claude-opus-5`, `gpt-6-astra`) match no real vendor lineup, which suggests a
relabeling proxy rather than a direct relationship with the model owners. That
is a reason not to pursue any commercial relationship with LLM7, not just a
reason it is currently unsupported as a profile.

## Policy, which holds whether or not a link ever exists

1. **A referral link is always disclosed.** `Attribution.Line` and
   `Attribution.Short` append `(referral link)` whenever `Affiliate` is set.
   There is no code path that emits one without the marker, and
   `TestReferralAlwaysDisclosed` fails the build if someone adds one.
2. **A user can always opt out.** Setting `GOPHERMIND_NO_AFFILIATE` to any
   non-empty value forces the plain provider website.
3. **A link's destination never changes silently.** Adding, removing, or
   repointing an `Affiliate` value requires a CHANGELOG entry naming the
   provider.
4. **Provider ranking is never influenced by revenue.** `Compats()` sorts by
   whether a key is needed, then whether the provider is supported, then
   alphabetically. Revenue is not an input and must not become one.
5. **No link is added without reading the program terms**, specifically
   whether the program requires disclosure language we are not using, and
   whether it permits use in an open-source tool.

## Outreach order, when the time comes

Ranked by plausible revenue against effort. All three are inference
marketplaces where a referred user may go on to spend, which is the only shape
of provider here where an affiliate arrangement makes sense at all.

1. **OpenRouter** - the largest paid surface of any provider in the registry,
   and the one most likely to convert a free-tier user into a paying one.
   Contact through their support channel; there is no public program to sign
   up for.
2. **Hugging Face** - a credit-metered free tier that naturally leads to paid
   Inference Providers. Their partner program is the route to ask about.
3. **NVIDIA (build.nvidia.com)** - the NVIDIA Developer Program is the
   existing relationship; ask whether it has any referral component.

Not worth approaching: the no-key providers (nothing to refer), the
national-cloud providers (ModelScope, SiliconFlow, Z AI), Cohere (whose free
tier is explicitly non-commercial), and LLM7 (see above).

## What to do first

Read each program's terms against policy items 4 and 5 above before
contacting anyone. If a program requires ranking influence or forbids
disclosure, decline it. The disclosure marker is not negotiable.

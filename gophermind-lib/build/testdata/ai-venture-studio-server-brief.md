---
spec_version: "2.0"
id: gm-2026-09-29-002
title: AI Venture Studio Server
language: go
repo: ~/src/venture-studio-server
base_branch: main
landing: commit
on_ambiguity: halt
milestone_approvals: true
secrets:
  - name: DATABASE_URL
    purpose: Postgres connection string for the studio database
  - name: JWT_SIGNING_KEY
    purpose: HMAC key for access and refresh tokens
  - name: STUDIO_LLM_API_KEY
    purpose: API key for the OpenAI-compatible LLM endpoint used for intake conversations and summaries
  - name: TEST_DATABASE_URL
    purpose: Postgres connection string for integration tests only; unset skips them
env:
  - name: STUDIO_LLM_BASE_URL
    purpose: OpenAI-compatible chat completions base URL
    default: https://api.groq.com/openai/v1
  - name: STUDIO_LLM_MODEL
    purpose: Model ID for intake, summaries, plans, and marketing generation
    default: llama-3.3-70b-versatile
  - name: DATA_DIR
    purpose: Root directory for the local BlobStore
    default: ./data
  - name: LISTEN_ADDR
    purpose: HTTP listen address
    default: ":8080"
  - name: LOG_LEVEL
    purpose: slog level
    default: info
  - name: STUDIO_FAKE_NOW
    purpose: RFC 3339 clock override, honored only in test builds
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
  - host: api.groq.com
    purpose: LLM endpoint for intake and summaries (tests use a fake)
    critical: false
budget:
  max_context_tokens: 8000
  max_revisions: 2
---

## Overview

The AI Venture Studio Server is the coordinating backend for AmTech's venture studio. Multiple companies,
each with multiple users, bring venture ideas into the system, get an AI-generated summary that frames the
opportunity, and then move each venture through a fixed pipeline from intake to exit. A venture can be
software, a physical product, a service, or anything else that could make money. The server owns all
data, all AI calls, all pipeline rules, and a versioned REST API that the Swift desktop and mobile apps
consume. For software ventures it can also generate a GopherMind brief and track the resulting build.
This brief covers the server only; the Swift clients are separate briefs that target this API.

## Features

### Accounts and companies

Users sign up with email and password, create or join companies, and hold a role in each company. A
user may belong to many companies. All venture data is scoped to a company and never visible across
companies.

Acceptance criteria:

- `POST /v1/auth/signup` with a new email and a password of 12 or more characters returns 201 with a user
  and an access/refresh token pair. A shorter password returns 400 `PASSWORD_TOO_SHORT`. A used email
  returns 409 `EMAIL_TAKEN`.
- `POST /v1/auth/login` returns tokens on a correct password and 401 `BAD_CREDENTIALS` on a wrong one,
  with the same response time within 50ms either way.
- `POST /v1/auth/refresh` exchanges a valid refresh token for a new pair and revokes the old refresh
  token. Reuse of a revoked refresh token revokes the whole token family and returns 401 `TOKEN_REUSED`.
- `POST /v1/companies` creates a company and makes the caller its `owner`.
- `POST /v1/companies/{id}/invites` by an `owner` or `admin` creates an invite for an email with a role
  of `admin` or `member`, returning an invite token. `POST /v1/invites/{token}/accept` by a logged-in user
  whose email matches adds the membership. A mismatched email returns 403 `INVITE_EMAIL_MISMATCH`.
- `GET /v1/me` returns the user and every membership with its role.
- Every request that touches company data requires the `X-Company-ID` header; a user without a
  membership in that company receives 404, not 403, so company IDs are not enumerable.
- Roles: `owner` can do everything including delete the company and change roles; `admin` can manage
  members and all ventures; `member` can create and edit ventures they are assigned to and read all
  ventures in the company.

### Venture intake

A venture enters the system in one of three ways: a free-text paragraph, a structured form, or a
guided conversation with the AI. All three produce the same `Venture` record in the `intake` stage
plus an `IntakeSession` that preserves exactly what was provided.

Acceptance criteria:

- `POST /v1/ventures` with `{"mode": "paragraph", "text": "..."}` creates a venture named from the first
  sentence (max 80 chars) and stores the text on the intake session. Text under 40 characters returns
  400 `INTAKE_TOO_SHORT`.
- `POST /v1/ventures` with `{"mode": "form", "form": {...}}` accepts the fields: `name`, `venture_type`
  (`software`, `physical_product`, `service`, `content`, `other`), `problem`, `customer`, `offering`,
  `revenue_model`, `why_us`, `known_risks`. `name`, `problem`, and `customer` are required; the rest are
  optional. Missing required fields return 400 `FIELD_REQUIRED` with the field name.
- `POST /v1/ventures` with `{"mode": "conversation"}` creates the venture in the `intake` stage with an
  open intake session and returns the first AI question.
- `POST /v1/ventures/{id}/intake/messages` appends a user message to an open conversation session and
  returns the next AI message. The AI asks one question at a time, covers at least: problem, customer,
  offering, revenue model, venture type, what exists already, and what the founder wants help with. When
  the AI has enough it responds with `{"done": true}` and the session closes. The transcript is stored
  in full. Posting to a closed session returns 409 `SESSION_CLOSED`.
- Every intake session records `mode`, `started_at`, `completed_at`, and the raw input or transcript.
  Intake sessions are immutable after completion.
- `GET /v1/ventures/{id}/intake` returns the session.

### Venture summary

After intake, the server generates a structured summary that guides everything downstream. The summary
is a versioned document. Users can edit it, regenerate it, and approve a version. Approval moves the
venture from `intake` to `summary`.

Acceptance criteria:

- `POST /v1/ventures/{id}/summary/generate` calls the LLM with the intake content and stores a new
  summary version with these fields: `one_liner`, `problem`, `customer`, `offering`, `venture_type`,
  `revenue_model`, `required_capabilities` (array of strings), `key_risks` (array of strings),
  `open_questions` (array of strings), `recommended_next_steps` (array of strings), `confidence`
  (`low`, `medium`, `high`). The LLM is instructed to return JSON only; a response that fails to parse is
  retried once, then returns 502 `LLM_BAD_RESPONSE`.
- `PUT /v1/ventures/{id}/summary` with any subset of the fields creates a new version marked
  `source: "user"`. Versions are never overwritten.
- `GET /v1/ventures/{id}/summary` returns the latest version; `?version=N` returns a specific one;
  `GET /v1/ventures/{id}/summary/versions` lists them with author and timestamp.
- `POST /v1/ventures/{id}/summary/{version}/approve` marks that version approved, sets the venture's
  `venture_type` from it, and advances the stage to `summary`. Approving when the venture is not in
  `intake` returns 409 `WRONG_STAGE`.
- Generating a summary creates a task "Review and approve summary" (origin `summary_approval`) for the
  venture owner; approving any version completes it.
- Generation is limited to 10 per venture per hour; the 11th returns 429 `RATE_LIMITED`.

### Pipeline stages

Every venture moves through fixed stages in order: `intake`, `summary`, `validation`, `plan`, `build`,
`launch`, `operate`, `exit`. Each stage has a checklist derived from a template for the venture's type.
A venture can advance only when every required checklist item is done, and can be sent back one stage
by an `admin` or `owner` with a reason. Every transition is recorded.

Acceptance criteria:

- `GET /v1/ventures/{id}/stage` returns the current stage, the checklist with each item's status, and
  whether the venture is eligible to advance.
- `POST /v1/ventures/{id}/stage/advance` moves to the next stage when all required items are done, else
  returns 409 `CHECKLIST_INCOMPLETE` listing the open required items.
- `POST /v1/ventures/{id}/stage/revert` with `{"reason": "..."}` moves back exactly one stage. A reason
  under 10 characters returns 400. Reverting from `intake` returns 409.
- `GET /v1/ventures/{id}/stage/history` lists every transition with actor, from, to, reason, and time.
- Checklist templates are stored per `venture_type` per stage as data, seeded by migration, editable by
  an `owner` via `PUT /v1/companies/{id}/templates/{venture_type}/{stage}`. Editing a template affects
  ventures that enter that stage afterwards, never ventures already in it.
- Seed templates exist for all five venture types and all eight stages. Minimum seeded content:
  `validation` requires "Customer interviews completed" and "Willingness to pay evidence recorded";
  `plan` requires "Business plan approved", "Base projection scenario created", "Revenue streams defined", "Entity structure decided", and "Owner assigned"; `build` for `software` requires "GopherMind
  brief approved"; `launch` requires "Entity formation complete or DBA filed", "Marketing plan approved", and "Launch checklist signed off"; `operate` requires "First P&L month closed"; `exit` requires "Exit path chosen"
  (`sell`, `hold`, `shut_down`).
- Entering a stage creates one task per checklist item (origin `stage_checklist`) assigned to the
  venture owner with the default due date; items can be reassigned by reassigning the task. Marking
  the task done completes the item and vice versa. Checklist items can be marked done or undone by any
  `member` assigned to the venture, or any `admin`/`owner`. Each change records who and when.

### Tasks

Every piece of work a human must do is a task with an owner and a due date. Tasks are created by hand
or by the system, and the system-created ones are what make the pipeline accountable: nothing that
requires a person is ever just a checkbox waiting for someone to notice it.

Acceptance criteria:

- `POST /v1/ventures/{id}/tasks` creates a task with `title`, optional `description`, `assignee_id`
  (must be a member of the company), `due_at`, `priority` (`low`, `normal`, `high`), and `status`
  defaulting to `open`. Manually created tasks may omit `assignee_id` and `due_at`.
- System-created tasks always have `assignee_id` and `due_at`. `origin` records what created them:
  `manual`, `stage_checklist`, `formation`, `plan_approval`, `summary_approval`, `campaign`,
  `pnl_close`, `kpi_missed`, `gophermind_escalated`. `origin_ref` holds the ID of the source item.
- Status values: `open`, `in_progress`, `blocked`, `done`, `cancelled`. `PATCH /v1/tasks/{id}`
  changes any field. A `blocked` task requires `blocked_reason`. Marking a system-created task `done`
  completes its source item (checklist item, formation item) in the same transaction, and completing
  the source item marks the task `done`; the two never disagree.
- Reassignment and due-date changes are recorded in `TaskHistory` with actor, old value, new value,
  and time. `GET /v1/tasks/{id}/history` lists it. A due date may only be moved later by an `admin` or
  `owner`, or by the assignee with a `reason` of 10 or more characters; the reason is stored.
- `GET /v1/ventures/{id}/tasks?status=&assignee=&overdue=true` filters. `GET /v1/me/tasks` lists the
  caller's open tasks across every company they belong to, grouped by company, ordered by due date,
  with `overdue` and `due_in_hours` computed.
- Assigning a task to a user who is not a company member returns 400 `NOT_A_MEMBER`.
- Default assignee for system-created tasks is the venture `owner_id`; default due date is set per
  origin by the company's `TaskDefaults` (`PUT /v1/companies/{id}/task-defaults`), seeded as:
  stage checklist 14 days, formation item 7 days, approvals 5 days, P&L close 10 days after month end,
  KPI missed 3 days, GopherMind escalation 2 days. A venture with no owner cannot advance past
  `summary`; `stage/advance` returns 409 `OWNER_REQUIRED`.

### Reminders and accountability

The server nags. Every task with a due date produces reminders on a schedule, overdue tasks escalate up
the chain, and every user and every venture has an accountability record that cannot be edited.

Acceptance criteria:

- Reminder schedule per task, relative to `due_at`: 7 days before (only if the task was created more
  than 7 days before due), 2 days before, the morning of (09:00 in the assignee's `timezone`, a user
  field defaulting to `America/Los_Angeles`), and then daily while overdue. Each reminder is a
  `Notification` row and an activity event. A task moved to `done` or `cancelled` cancels its pending
  reminders.
- Escalation: 2 days overdue notifies the venture owner in addition to the assignee; 5 days overdue
  notifies every company `admin` and `owner`; 10 days overdue creates a `high` priority task for the
  venture owner titled "Resolve overdue: <task title>" with `origin: escalation`. Escalation
  thresholds are part of `TaskDefaults` and editable per company.
- `GET /v1/me/notifications?unread=true` lists the caller's notifications newest first;
  `POST /v1/notifications/{id}/read` and `POST /v1/notifications/read-all` mark them. Each notification
  carries `type` (`reminder`, `overdue`, `escalation`, `assigned`, `reassigned`, `mention`), `task_id`,
  `venture_id`, `message`, `created_at`, `read_at`.
- Notifications are also pushed on the user's SSE stream `GET /v1/me/events/stream` within 1 second,
  and delivered through a `Notifier` interface with two in-tree implementations: `inapp` (always on)
  and `webhook` (POSTs a signed JSON payload to a URL configured per company via
  `PUT /v1/companies/{id}/notification-webhook`, with HMAC-SHA256 in `X-Studio-Signature`, retried 3
  times with backoff, failures logged). Email and mobile push are additional `Notifier`
  implementations for a later brief; the interface is the contract.
- `GET /v1/companies/{id}/accountability?from=&to=` returns, per member: tasks assigned, completed,
  completed on time, completed late, still overdue, average days late, and a list of currently overdue
  tasks. `GET /v1/ventures/{id}/accountability` returns the same per venture. Both are computed from
  `TaskHistory` and are read-only.
- `GET /v1/me/accountability` returns the caller's own record.
- A weekly digest job creates one `digest` notification per user every Monday 08:00 in their timezone
  summarizing open tasks due this week, overdue tasks, and last week's completions.
- The reminder and escalation jobs run every 5 minutes, are idempotent (a reminder for a given task and
  schedule slot is created at most once, enforced by a unique index), and tolerate the server restarting
  mid-run.

### Documents

Files and links attached to a venture, versioned by filename.

Acceptance criteria:

- `POST /v1/ventures/{id}/documents` accepts multipart upload up to 50 MB or `{"url": "..."}` for a link.
  Files are stored on local disk under `DATA_DIR/companies/{company}/ventures/{venture}/{doc}/{version}`
  behind a `BlobStore` interface so S3 can replace it later.
- Uploading a file with the same filename creates a new version; `GET /v1/documents/{id}` returns the
  latest and `GET /v1/documents/{id}/versions` lists all.
- `GET /v1/documents/{id}/content` streams the file with the stored content type. A user without
  membership in the owning company receives 404.
- Deleting a document is soft: `DELETE` marks it deleted and it disappears from lists; content is
  retained for 30 days then purged by a scheduled job.

### GopherMind handoff

For ventures with `venture_type: software`, the server can draft a GopherMind v2 brief from the approved
summary and record the build's progress.

Acceptance criteria:

- `POST /v1/ventures/{id}/gophermind/brief` calls the LLM with the approved summary and the GopherMind
  brief specification (embedded in the binary as `spec/gophermind-brief-v2.md`) and stores a draft brief
  as a document named `gophermind-brief.md`. Calling it on a non-software venture returns 409
  `NOT_SOFTWARE`. Calling it before a summary is approved returns 409 `SUMMARY_NOT_APPROVED`.
- The draft's frontmatter validates against the embedded `spec/brief-frontmatter.schema.json`; if the
  LLM output does not validate, the server fixes `spec_version`, `id`, `title`, and `language` itself
  and marks the document `needs_review: true`.
- `POST /v1/ventures/{id}/gophermind/runs` records a run with `run_id`, `repo`, `started_at`.
  `PATCH /v1/gophermind/runs/{run_id}` accepts `status` (`running`, `verified`, `failed`, `escalated`)
  and an optional `report` JSON object matching GopherMind's `report.json`. This is how GopherMind, or a
  human, reports progress; the server does not call GopherMind.
- `GET /v1/ventures/{id}/gophermind/runs` lists runs with status and the latest report summary
  (nodes verified, failed, escalated).
- A run reported as `escalated` creates a task "Resolve GopherMind escalation on <run_id>" (origin
  `gophermind_escalated`) for the venture owner; a later `running` or `verified` status completes it.

### Entity formation

Every venture that will operate as its own legal entity, or under a DBA of an existing entity, is
tracked from decision to completed filing. The server does not file with any state; it decides what is
needed, produces the paperwork, and records what a human filed and when.

Acceptance criteria:

- `POST /v1/ventures/{id}/entity` sets the operating structure: `{"structure": "new_llc" | "existing_entity"
  | "dba", "state": "TX", "parent_entity_id": null | "..."}`. `dba` and `existing_entity` require
  `parent_entity_id` referencing an entity in the company's entity registry; missing returns 400
  `PARENT_REQUIRED`.
- `GET /v1/companies/{id}/entities` and `POST /v1/companies/{id}/entities` manage the company's
  entity registry: `legal_name`, `entity_type` (`llc`, `corp`, `sole_prop`, `partnership`), `state`,
  `ein_last4`, `formed_at`, `registered_agent`, `status` (`planned`, `filing`, `active`, `dissolved`).
- Setting the structure creates a formation checklist from a template keyed by `structure` and `state`,
  and one task per required item (origin `formation`) assigned to the venture owner, due dates spaced
  in template order so items that depend on earlier ones (EIN after formation filing) are due later.
  Seeded templates exist for all 50 states plus DC for `new_llc` and `dba` with these items at minimum:
  name availability checked, name reserved (optional), registered agent chosen, formation document
  filed, EIN obtained, operating agreement signed, state tax registration, local business license,
  bank account opened, DBA published where the state requires it. Items carry `required`, `filing_fee_cents`
  (nullable), `authority` (who you file with), `url` (the authority's page), and `notes`.
- `PATCH /v1/ventures/{id}/entity/checklist/{key}` marks an item done with `filed_at`, optional
  `confirmation_number`, and an optional document ID. Marking the formation document filed sets the
  registry entity to `filing`; marking EIN obtained sets it to `active`.
- `POST /v1/ventures/{id}/entity/documents/generate` with `{"kind": "operating_agreement" |
  "dba_statement" | "member_consent"}` produces a Markdown draft from templates plus the venture's
  ownership table and stores it as a document with `needs_review: true`. Every generated legal document
  carries a header stating it is a draft for attorney review.
- `GET /v1/ventures/{id}/entity` returns structure, registry entity, checklist with status, and all
  generated documents.

### Ownership

A cap table per venture. Owners can be people or entities in the company's registry. Every change is a
dated transaction, never an edit, so ownership at any date can be reconstructed.

Acceptance criteria:

- `GET /v1/ventures/{id}/ownership` returns current holders with `holder_type` (`user`, `entity`,
  `external`), `holder_name`, `units`, `percent` (derived, 4 decimal places), `class` (`common`,
  `preferred`, `profit_interest`), and `vesting` (`null` or `{total_units, vested_units, cliff_at,
  fully_vested_at, schedule: "monthly" | "quarterly" | "annual"}`).
- `POST /v1/ventures/{id}/ownership/transactions` records one of: `issue`, `transfer`, `cancel`,
  `vest`, with `holder`, `units`, `class`, `effective_at`, `consideration_cents` (nullable), `note`.
  A transfer requires `from_holder` and `to_holder` and fails with 409 `INSUFFICIENT_UNITS` if the
  source does not hold enough units at `effective_at`.
- `GET /v1/ventures/{id}/ownership?as_of=2026-01-01` reconstructs the table at that date.
- `GET /v1/ventures/{id}/ownership/transactions` lists transactions with actor and time recorded.
- Percentages across all holders sum to 100.0000 within rounding, and a test asserts this after every
  transaction type.
- A scheduled job runs daily and records `vest` transactions for any vesting schedule whose next
  vesting date has passed.
- Exporting `GET /v1/ventures/{id}/ownership/export.csv` returns the current table as CSV.

### Business plan and prospectus

Two long-form documents generated from the approved summary and the venture's financial data, edited
section by section, versioned, and exportable. The business plan is internal; the prospectus is what
you hand an investor or buyer.

Acceptance criteria:

- `POST /v1/ventures/{id}/plans/{kind}/generate` where `kind` is `business_plan` or `prospectus` asks
  the LLM for each section separately (one call per section, so each call fits a small context) and
  stores a new plan version. Business plan sections: executive summary, problem, solution, market,
  competition, business model, go-to-market, operations, team and ownership, financial plan, risks,
  milestones. Prospectus sections: opportunity, offering, use of funds, financial highlights, ownership
  and terms, risk factors, exit strategy. The financial sections are filled from the projections and
  P&L features below, not invented by the LLM; the LLM writes the prose around numbers the server
  supplies.
- `PUT /v1/ventures/{id}/plans/{kind}/sections/{section}` replaces one section's Markdown and creates a
  new version. Versions record author, source (`llm` or `user`), and time.
- `GET /v1/ventures/{id}/plans/{kind}` returns the latest version assembled as one Markdown document
  with a generated table of contents; `?format=md` downloads it as a file. `?version=N` selects a version.
- `POST /v1/ventures/{id}/plans/{kind}/{version}/approve` marks it approved. The `plan` stage checklist
  item "Business plan approved" is completed automatically when a business plan version is approved.
- A prospectus cannot be generated until a business plan version is approved; returns 409
  `BUSINESS_PLAN_NOT_APPROVED`.
- Generating either document creates a "Review and approve <kind>" task (origin `plan_approval`) for
  the venture owner; approval completes it.

### Marketing

The marketing plan and the information needed to execute it: positioning, personas, channels, and
tracked campaigns.

Acceptance criteria:

- `GET /v1/ventures/{id}/marketing` and `PUT /v1/ventures/{id}/marketing` manage a single marketing
  record with: `positioning_statement`, `value_props` (array), `brand_voice`, `personas` (array of
  `{name, description, pains, gains, where_they_are}`), `channels` (array of `{channel, role,
  monthly_budget_cents, owner_id, status}`), `launch_date`.
- `POST /v1/ventures/{id}/marketing/generate` drafts every field from the approved summary via the LLM
  and stores it; existing user-edited fields are not overwritten unless `{"overwrite": true}`.
- `POST /v1/ventures/{id}/marketing/campaigns` creates a campaign with `name`, `channel`,
  `starts_at`, `ends_at`, `budget_cents`, `goal_kpi_key` (references a KPI on the venture), `goal_value`.
  `PATCH` updates `spend_cents` and `status` (`planned`, `live`, `paused`, `done`).
- `GET /v1/ventures/{id}/marketing/campaigns` lists campaigns with the current value of each
  campaign's goal KPI beside its goal.
- Creating a campaign creates a launch task due `starts_at` and a wrap-up task due `ends_at` plus 2
  days (origin `campaign`) for the campaign's channel owner, falling back to the venture owner.

### Revenue streams

Every way the venture makes money, defined once and referenced by projections, KPIs, and P&L lines.

Acceptance criteria:

- `POST /v1/ventures/{id}/revenue-streams` creates a stream with `name`, `model` (`one_time`,
  `subscription`, `usage`, `commission`, `advertising`, `licensing`, `service_hours`, `other`),
  `unit_label` (e.g. seat, order, hour), `unit_price_cents`, `billing_period` (`null`, `monthly`,
  `annual`), `cogs_percent` (0 to 100, decimal), `status` (`planned`, `active`, `retired`), `notes`.
- `GET`, `PATCH`, and retire (`status: retired`) are supported. A stream referenced by any projection or
  P&L line cannot be deleted, only retired.
- `POST /v1/ventures/{id}/revenue-streams/suggest` asks the LLM for candidate streams from the
  approved summary and returns them as suggestions; nothing is saved until the client posts them.

### KPIs

Definitions, targets, and time-series actuals for the numbers that matter to the venture.

Acceptance criteria:

- `POST /v1/ventures/{id}/kpis` defines a KPI: `key` (unique per venture, snake_case), `name`,
  `unit` (`count`, `cents`, `percent`, `ratio`, `days`), `direction` (`higher_is_better`,
  `lower_is_better`), `period` (`daily`, `weekly`, `monthly`), `target_value`, `source` (`manual`,
  `derived`), `formula` (for derived: an expression over other KPI keys and revenue stream fields using
  `+ - * /` and parentheses, evaluated by an in-tree evaluator; a formula referencing an unknown key
  returns 400 `UNKNOWN_KPI_REF`).
- Seed KPI templates per venture type are applied when the venture reaches `plan`, at minimum:
  `monthly_revenue_cents`, `monthly_cost_cents`, `gross_margin_percent` (derived), `customers`,
  `cac_cents`, `churn_percent` for software and subscription streams, `units_sold` for physical
  products, `billable_hours` and `utilization_percent` for services.
- `POST /v1/ventures/{id}/kpis/{key}/values` records `{period_start, value}`; posting the same period
  again replaces the value and records the previous one in history.
- `GET /v1/ventures/{id}/kpis` returns every KPI with target, latest value, previous value, and
  percent change. `GET /v1/ventures/{id}/kpis/{key}/values?from=&to=` returns the series.
- Derived KPIs are recomputed on read from their inputs for the requested period.
- When a recorded value misses `target_value` in the KPI's `direction`, a task "Address <KPI name>
  miss for <period>" (origin `kpi_missed`) is created for the venture owner, once per KPI per period.

### Projections

A monthly forward model for up to 60 months, built from revenue streams and cost lines, with named
scenarios.

Acceptance criteria:

- `POST /v1/ventures/{id}/projections` creates a scenario: `name` (e.g. base, upside, downside),
  `start_month` (YYYY-MM), `months` (12 to 60).
- `PUT /v1/ventures/{id}/projections/{scenario}/streams/{stream_id}` sets per-month assumptions for a
  revenue stream: `units` array and optional `unit_price_cents` overrides array, each of length
  `months`. Revenue per month is `units * unit_price_cents`; COGS is `revenue * cogs_percent / 100`.
- `PUT /v1/ventures/{id}/projections/{scenario}/costs` sets cost lines: array of `{name, category
  (payroll, contractors, software, marketing, rent, legal, other), amounts_cents: [...]}` each of
  length `months`.
- `GET /v1/ventures/{id}/projections/{scenario}` returns the computed model per month: revenue by
  stream, total revenue, COGS, gross profit, costs by category, total costs, net income, cumulative net
  income, and cash position given `starting_cash_cents` on the scenario. Also returns `break_even_month`
  (first month cumulative net income is non-negative, or null) and `runway_months` (months until cash
  goes negative, or null).
- `GET /v1/ventures/{id}/projections/compare` returns the totals of every scenario side by side.
- `POST /v1/ventures/{id}/projections/{scenario}/clone` with a new name copies all assumptions.
- All arithmetic is integer cents; percentages are applied with round-half-even; a test checks that
  monthly lines sum exactly to totals.

### Profit and loss

Monthly actuals entered per venture, a generated P&L statement for any period, and variance against
the chosen projection scenario.

Acceptance criteria:

- `POST /v1/ventures/{id}/pnl/entries` records an actual: `{month (YYYY-MM), line_type (revenue,
  cogs, expense), revenue_stream_id (required for revenue and cogs), category (required for expense,
  same list as projection costs), amount_cents, memo}`. Multiple entries per month per line are allowed
  and summed.
- `GET /v1/ventures/{id}/pnl?from=YYYY-MM&to=YYYY-MM` returns a statement per month and for the whole
  period: revenue by stream, total revenue, COGS by stream, gross profit, gross margin percent, expenses
  by category, total expenses, net income, plus year-to-date columns when the range crosses a calendar
  year.
- `GET /v1/ventures/{id}/pnl/variance?scenario=&from=&to=` returns actual, projected, and difference
  (absolute and percent) for every line.
- `POST /v1/ventures/{id}/pnl/import.csv` accepts a CSV with columns `month,line_type,stream_or_category,
  amount,memo`, validates every row before writing any, and returns row-level errors on failure.
- `GET /v1/ventures/{id}/pnl/export.csv` exports entries.
- Recording an actual for `monthly_revenue_cents` or `monthly_cost_cents` automatically writes the
  matching KPI value for that month, so KPI and P&L never disagree.
- A closed month (`POST /v1/ventures/{id}/pnl/close/{month}` by an `admin` or `owner`) rejects further
  entries with 409 `MONTH_CLOSED` unless reopened.
- On the first day of each month, every venture in `operate` or `launch` gets a task "Close P&L for
  <previous month>" (origin `pnl_close`) for the venture owner, due per `TaskDefaults`; closing the
  month completes it.

### Portfolio

Company-level view of every venture with stage, type, owner, and manually entered financial fields.

Acceptance criteria:

- `GET /v1/companies/{id}/portfolio` returns every venture with `stage`, `venture_type`, `owner`,
  `created_at`, `updated_at`, and `financials`, plus stage counts and type counts for the company.
- Filters: `?stage=`, `?type=`, `?owner=`, `?q=` (name and one-liner contains, case-insensitive).
  Pagination with `?limit=` (default 50, max 200) and `?cursor=`.
- `financials` on each venture is derived, not entered: `last_month_revenue_cents` and
  `last_month_cost_cents` from the P&L, `trailing_12_revenue_cents`, `invested_cents` from ownership
  transactions with consideration, and `valuation_estimate_cents` which is the one manually set field
  via `PUT /v1/ventures/{id}/valuation`. Negative values return 400.
- `GET /v1/companies/{id}/portfolio/summary` returns totals of each financial field across ventures
  not in `exit`, and separately for ventures in `exit`.

### Activity and events

Every meaningful change on a venture produces an activity event. Clients can list events and subscribe
to a live stream.

Acceptance criteria:

- Events are recorded for: venture created, intake completed, summary generated, summary approved,
  stage advanced, stage reverted, checklist item changed, task created, task status changed, document
  added, GopherMind run status changed, member added, member role changed, entity structure set,
  formation item filed, ownership transaction recorded, plan version approved, campaign status changed,
  KPI target missed (evaluated when a value is recorded), P&L month closed, task assigned, task
  reassigned, task due date changed, task overdue, task escalated, reminder sent.
- `GET /v1/ventures/{id}/activity` and `GET /v1/companies/{id}/activity` list events newest first with
  cursor pagination.
- `GET /v1/companies/{id}/events/stream` is a Server-Sent Events endpoint. A connected client receives
  every event for that company within 1 second of it being recorded. The stream sends a `: keepalive`
  comment every 20 seconds. Reconnection with `Last-Event-ID` replays missed events from the last 24
  hours.
- Events carry `id`, `type`, `company_id`, `venture_id` (nullable), `actor_id`, `occurred_at`, and a
  `data` object specific to the type.

### API contract and operations

The REST API is the product for the Swift clients. It must be discoverable, consistent, and stable.

Acceptance criteria:

- All endpoints live under `/v1`. Request and response bodies are JSON with `Content-Type:
  application/json`. Errors always have the shape `{"error": {"code": "...", "message": "...",
  "field": "..."}}` with `field` present only for validation errors.
- `GET /v1/openapi.json` returns an OpenAPI 3.1 document generated from the route table and request
  and response types at build time and embedded in the binary. Every endpoint in this brief appears in it
  with request and response schemas.
- IDs are UUID v7 strings. Timestamps are RFC 3339 in UTC.
- `GET /healthz` returns 200 `ok` without touching the database. `GET /readyz` returns 200 only when
  the database responds to `SELECT 1` within 500ms.
- `GET /metrics` exposes request count, latency histogram, and error count per route in Prometheus text
  format, using a small in-tree exporter, not a third-party client.
- Migrations are SQL files embedded in the binary and applied with `venture-server migrate up`. The
  server refuses to start if migrations are pending. `migrate down` reverts one.
- Configuration is by environment variables only: `DATABASE_URL`, `JWT_SIGNING_KEY`,
  `STUDIO_LLM_API_KEY`, `STUDIO_LLM_BASE_URL`, `STUDIO_LLM_MODEL`, `DATA_DIR`, `LISTEN_ADDR` (default
  `:8080`), `LOG_LEVEL`. Missing required variables produce a single clear error at startup listing all
  of them.
- Structured JSON logs via `log/slog`. Every request logs method, route, status, duration, company ID,
  and user ID. Never bodies, never tokens, never passwords, never email addresses in log lines.

## Architecture

Module: `github.com/amtechhq/venture-studio-server`. Go 1.22 with the standard library `net/http`
mux for routing.

Packages:

- `cmd/venture-server`: main. Subcommands `serve`, `migrate up`, `migrate down`, `seed-templates`.
- `internal/config`: env loading and validation.
- `internal/db`: pgx pool, migration runner, embedded SQL files under `internal/db/migrations`.
- `internal/auth`: password hashing (argon2id), JWT issue and verify, refresh token families, middleware
  that sets user and company on the request context.
- `internal/tenancy`: company, membership, invite, role checks. Every repository function takes a
  `company_id` and every SQL query filters by it; there is no query path that omits it.
- `internal/venture`: venture record, intake sessions, stage machine, checklist, transitions.
- `internal/summary`: summary versions and the LLM prompt for generation.
- `internal/llm`: OpenAI-compatible chat client with a `Client` interface and a `Fake` implementation
  used by every test. Retries once on parse failure. Never logs prompts or completions.
- `internal/task`, `internal/document`, `internal/blob` (with `BlobStore` interface and a local disk
  implementation), `internal/portfolio`, `internal/activity` (event store plus SSE hub),
  `internal/gophermind` (brief drafting and run tracking), `internal/entity` (registry, formation
  checklists, legal document templates under `internal/entity/templates` and state data under
  `internal/entity/states`), `internal/ownership` (cap table transactions and as-of reconstruction),
  `internal/plan` (business plan and prospectus sections), `internal/marketing`, `internal/revenue`,
  `internal/kpi` (definitions, values, formula evaluator), `internal/projection` (scenario model),
  `internal/pnl` (entries, statements, variance, CSV import and export).
- `internal/httpapi`: one file per feature area with handlers as methods on `Server`, a route table
  that is the single source for both the mux and the OpenAPI generator, error mapping, pagination
  helpers.
- `internal/openapi`: generates the document from the route table and Go types at build time via
  `go generate`; the result is embedded.
- `internal/metrics`: in-tree Prometheus text exporter.
- `internal/task` also owns `TaskHistory` and the source-item sync; `internal/notify` owns
  notifications, the `Notifier` interface, `inapp`, `webhook`, reminder scheduling, and escalation;
  `internal/accountability` computes the read-only reports from `TaskHistory`.
- `internal/jobs`: scheduled work run from `serve` on tickers: reminders and escalation every 5
  minutes, purge of soft-deleted documents daily, vesting daily, monthly P&L close tasks, weekly digest.
  Jobs take a Postgres advisory lock so two server instances never run the same job concurrently.
- `internal/money`: integer-cents arithmetic helpers with round-half-even percent application, used by
  projection, pnl, and portfolio. No floats touch money anywhere in the codebase.

Data flow: request enters `httpapi`, auth middleware resolves user and company, handler calls one
service package, service calls repository functions in the same package using the pgx pool, writes
an activity event in the same transaction when the feature says so, and the SSE hub fans the event out
after commit.

Patterns to follow:

- Handlers are methods on `Server`. No package-level state except the embedded assets.
- Each feature package exposes a `Service` struct with methods taking `context.Context` first and a
  `Repo` interface that the `Service` depends on, so services are unit-testable with a fake repo and
  repos are tested against a real Postgres in integration tests.
- Errors: each package defines sentinel errors; `httpapi` maps them to HTTP status and error codes in
  one table. Validation errors are `*ValidationError{Code, Field, Message}`.
- All writes that emit events do so inside the same database transaction as the write.
- Tests: table-driven; unit tests use fake repos and the fake LLM; integration tests under
  `internal/db` and `internal/httpapi` run against Postgres from `TEST_DATABASE_URL` and are skipped
  when it is unset.

## Data

Company: `id`, `name`, `created_at`.

User: `id`, `email` (lowercased, unique), `password_hash`, `display_name`, `created_at`.

Membership: `user_id`, `company_id`, `role` (`owner`, `admin`, `member`), `created_at`. Primary key
(`user_id`, `company_id`).

Invite: `id`, `company_id`, `email`, `role`, `token_hash`, `invited_by`, `expires_at` (7 days),
`accepted_at`.

RefreshToken: `id`, `user_id`, `family_id`, `token_hash`, `expires_at` (30 days), `revoked_at`.

Venture: `id`, `company_id`, `name`, `venture_type` (nullable until summary approved), `stage`,
`owner_id`, `created_by`, `created_at`, `updated_at`, `financials` (JSONB with the four cents fields).

IntakeSession: `id`, `venture_id`, `mode`, `raw_text` (paragraph), `form` (JSONB), `transcript` (JSONB
array of `{role, content, at}`), `started_at`, `completed_at`.

SummaryVersion: `id`, `venture_id`, `version` (int, unique per venture), `source` (`llm`, `user`),
`author_id` (nullable for llm), `fields` (JSONB with the eleven summary fields), `approved_at`,
`approved_by`, `created_at`.

StageTemplate: `company_id`, `venture_type`, `stage`, `items` (JSONB array of `{key, label, required}`).
Seed rows have `company_id` null and act as defaults; a company row overrides.

ChecklistItem: `id`, `venture_id`, `stage`, `key`, `label`, `required`, `done_at`, `done_by`.

StageTransition: `id`, `venture_id`, `from_stage`, `to_stage`, `actor_id`, `reason`, `occurred_at`.

Task: `id`, `venture_id`, `title`, `description`, `assignee_id`, `status`, `priority`, `due_at`,
`origin`, `origin_ref`, `blocked_reason`, `created_by`, `created_at`, `updated_at`, `completed_at`.
TaskHistory: `id`, `task_id`, `field`, `old_value`, `new_value`, `reason`, `actor_id`, `occurred_at`.
TaskDefaults: `company_id`, `due_days` (JSONB map of origin to days), `escalation_days` (JSONB
`{owner, admins, escalation_task}`).

Notification: `id`, `user_id`, `type`, `task_id`, `venture_id`, `message`, `created_at`, `read_at`.
ReminderSent: `task_id`, `slot` (`d7`, `d2`, `d0`, or `overdue:YYYY-MM-DD`), `sent_at`; unique on
(`task_id`, `slot`). NotificationWebhook: `company_id`, `url`, `secret_hash`, `enabled`.

User gains `timezone` (IANA name, default `America/Los_Angeles`).

Document: `id`, `venture_id`, `name`, `kind` (`file`, `link`), `url` (for links), `deleted_at`,
`needs_review`, `created_at`. DocumentVersion: `id`, `document_id`, `version`, `content_type`,
`size_bytes`, `storage_key`, `uploaded_by`, `created_at`.

GopherMindRun: `id`, `venture_id`, `run_id`, `repo`, `status`, `report` (JSONB), `started_at`,
`updated_at`.

Entity: `id`, `company_id`, `legal_name`, `entity_type`, `state`, `ein_last4`, `formed_at`,
`registered_agent`, `status`. VentureEntity: `venture_id`, `structure`, `state`, `entity_id`,
`parent_entity_id`. FormationTemplate: `structure`, `state`, `items` (JSONB). FormationItem: `id`,
`venture_id`, `key`, `label`, `required`, `authority`, `url`, `filing_fee_cents`, `filed_at`,
`confirmation_number`, `document_id`, `done_by`.

OwnershipTransaction: `id`, `venture_id`, `type`, `holder_type`, `holder_id` (nullable),
`holder_name`, `from_holder_id`, `to_holder_id`, `units` (bigint), `class`, `effective_at`,
`consideration_cents`, `note`, `actor_id`, `recorded_at`. VestingSchedule: `id`, `venture_id`,
`holder_id`, `total_units`, `cliff_at`, `fully_vested_at`, `schedule`, `last_vested_at`.

PlanVersion: `id`, `venture_id`, `kind`, `version`, `sections` (JSONB map of section key to Markdown),
`source`, `author_id`, `approved_at`, `approved_by`, `created_at`.

Marketing: `venture_id`, `fields` (JSONB), `updated_at`. Campaign: `id`, `venture_id`, `name`,
`channel`, `starts_at`, `ends_at`, `budget_cents`, `spend_cents`, `goal_kpi_key`, `goal_value`, `status`.

RevenueStream: `id`, `venture_id`, `name`, `model`, `unit_label`, `unit_price_cents`, `billing_period`,
`cogs_percent` (numeric(5,2)), `status`, `notes`.

KPI: `id`, `venture_id`, `key`, `name`, `unit`, `direction`, `period`, `target_value` (numeric),
`source`, `formula`. KPIValue: `kpi_id`, `period_start`, `value` (numeric), `recorded_by`,
`recorded_at`; KPIValueHistory mirrors it with `replaced_at`.

ProjectionScenario: `id`, `venture_id`, `name`, `start_month`, `months`, `starting_cash_cents`.
ProjectionStreamAssumption: `scenario_id`, `stream_id`, `units` (JSONB int array), `unit_price_cents`
(JSONB nullable int array). ProjectionCostLine: `id`, `scenario_id`, `name`, `category`,
`amounts_cents` (JSONB int array).

PnlEntry: `id`, `venture_id`, `month`, `line_type`, `revenue_stream_id`, `category`, `amount_cents`,
`memo`, `recorded_by`, `recorded_at`. PnlClosedMonth: `venture_id`, `month`, `closed_by`, `closed_at`.

ActivityEvent: `id`, `company_id`, `venture_id`, `type`, `actor_id`, `data` (JSONB), `occurred_at`.
Indexed on (`company_id`, `occurred_at`) and (`venture_id`, `occurred_at`).

## Constraints

- Go 1.22 or later. `gofmt` and `go vet` clean.
- Allowed third-party modules and nothing else: `github.com/jackc/pgx/v5`, `golang.org/x/crypto`
  (argon2id only). Everything else, including JWT, UUID v7, SSE, OpenAPI generation, and Prometheus
  text output, is written in-tree against the standard library.
- Postgres 15 or later. All schema changes are migrations. No ORM.
- Every query that reads or writes company-scoped data includes `company_id` in its WHERE clause.
  A test scans all SQL under `internal/` for table names of scoped tables and fails if a statement
  lacks `company_id`.
- Passwords hashed with argon2id (64 MB, 3 iterations, 4 threads). Access tokens expire in 15 minutes,
  refresh tokens in 30 days.
- No request or response bodies, tokens, passwords, prompts, completions, or email addresses in logs.
- LLM calls have a 60 second timeout and are never made from within a database transaction.
- All list endpoints use cursor pagination; no offset pagination anywhere.
- Handlers never exceed 60 lines; move logic into services.
- Tests are table-driven and live next to the code they test. Integration tests require
  `TEST_DATABASE_URL` and skip cleanly without it.

## Out of scope

These are separate briefs or later phases, not things the studio will never do. GopherMind must not
build any of them here.

- The Swift desktop and mobile apps. They consume this API.
- Billing, subscriptions, or per-seat pricing.
- Email delivery and mobile push (invites are returned as tokens for the client to deliver; reminders
  reach users in-app, over SSE, and by webhook; email and APNs are `Notifier` implementations for a
  later brief).
- SSO, OAuth, or magic links.
- Calling GopherMind directly or triggering builds; the server only records runs reported to it.
- S3 or other remote blob storage (the interface exists; only local disk is implemented).
- Full-text search beyond the portfolio `q` filter.
- Any AI feature beyond intake conversation, summary generation, and GopherMind brief drafting.
- Public or cross-company sharing of ventures.
- Filing anything with a state, the IRS, or a bank. The server tracks and drafts; humans file.
- Legal review. Generated agreements are drafts flagged for attorney review, never final.
- Accounting integrations (QuickBooks, Xero, bank feeds). P&L actuals are entered or imported from CSV.
- Payroll, invoicing, or payment processing.
- PDF rendering of plans and prospectuses (Markdown export only; PDF is a client or later concern).
- Investor-facing portals or data rooms.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes with `TEST_DATABASE_URL` unset (unit tests only).
- With Postgres available and `TEST_DATABASE_URL` set, `go test ./... -tags integration` passes.
- `venture-server migrate up` on an empty database applies every migration and `venture-server
  seed-templates` inserts the default checklist templates; running both again is a no-op.
- `venture-server serve` starts and `curl -s localhost:8080/healthz` prints `ok`.
- `curl -s localhost:8080/v1/openapi.json | jq '.paths | length'` is at least 90.
- End-to-end with `STUDIO_LLM_BASE_URL` pointed at the fake LLM server started by
  `venture-server fake-llm`: signup, create company, create venture in paragraph mode, generate summary,
  approve it, advance to `validation`, and an SSE client connected to the company stream receives
  `venture.created`, `summary.generated`, `summary.approved`, and `stage.advanced` in that order.
- Financial round trip: create a revenue stream, a base projection with 12 months, three P&L entries,
  then `GET /pnl/variance` returns a non-zero difference on the revenue line for the entered months and
  the projection's totals equal the sum of its monthly lines to the cent.
- Ownership round trip: issue 1,000,000 units to two holders, transfer 100,000, and `GET /ownership`
  percentages sum to 100.0000; `?as_of=` before the transfer shows the original split.
- Accountability round trip: advance a venture into `validation`, confirm one task exists per required
  checklist item assigned to the owner with a due date; set the clock (via `STUDIO_FAKE_NOW` in test
  builds) to 3 days past one task's due date, run the reminder job, and confirm the assignee has a
  `reminder`, the owner has an `overdue` notification, and the company accountability report shows one
  overdue task for that member.
- Two users in different companies cannot read each other's ventures: the second user's `GET` on the
  first user's venture ID returns 404.

# GopherMind Development Rules

## Core Mindset
- Default to action: decompose large tasks, find next concrete step, take it
- Think before coding: don't assume, surface tradeoffs, ask if confused
- One step at a time: run and test each step before moving on

## Work Rules
- Define verifiable success upfront: transform vague tasks into testable goals
- Write tests FIRST (test-driven): tests define expected behavior before implementation
- Commit after each completed step with descriptive message
- Surgical changes only: touch only what you must, match existing style

## Go Project Standards
- Prefer Go for backend, CLI, internal tooling
- Parameterized queries only, never string concatenation
- Business logic in service/model layers, not controllers
- Config-driven: store settings in tables/files, not hardcoded
- No error handling for impossible scenarios

## Design Philosophy
- Simplicity first: minimum code solving the problem, nothing speculative
- No abstractions for single-use code
- No "flexibility" that wasn't requested
- Explicit over magic: transparent code beats framework cleverness
- Security by default: built into every layer

## Phase/Task Execution (TDD-First)
1. Decompose large plans into < 30 min subtasks
2. Write tests that define expected behavior BEFORE implementing
3. Implement code to make tests pass
4. Verify tests pass, commit when done
5. Escalate after 2+ failed attempts
6. Check git status/log ONCE per task, not repeatedly — it doesn't change
   between your own shell calls. Re-check only after you've made an edit.
7. Before grepping for a symbol, check the task's acceptance criteria first:
   if it says "define X", X may already exist — verify with one search, then
   move to closing the actual gap (see the task's own acceptance_criteria).

## Context Budget
- Keep instructions under 6000 bytes so task has room
- Trim aggressively: cut verbose explanations, keep essence
- Migrate information to shorter form, never lose it

## Architecture & Tech Stack
- Backend/CLI: Go (preferred), Python (data/ETL), FastAPI/Flask
- Frontend: React + TypeScript
- Infra: CDK (TypeScript/Python), Terraform
- Database: MySQL (primary), Redis (caching)
- No speculative code: build what was asked, nothing more

## Git & Deployment
- Deploy via git: git fetch + git checkout on target, never scp files
- One commit per completed step with descriptive message
- Touch only what you must: don't refactor adjacent code
- Tests pass before and after every change

## MCP & RAG
- Query RAG BEFORE implementing model/controller/service/schema
- Store significant work summaries to RAG after completion
- Do not infer what you can retrieve

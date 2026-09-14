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

## Context Budget
- Keep instructions under 6000 bytes so task has room
- Trim aggressively: cut verbose explanations, keep essence
- Migrate information to shorter form, never lose it

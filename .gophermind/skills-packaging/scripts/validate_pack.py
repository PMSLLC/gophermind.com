#!/usr/bin/env python3
"""Check pack structure and run isolated Git/Go demonstrations.

This does not test GopherMind itself or score an LLM's behavior. All command
fixtures live in a temporary directory; no user repository is changed.
Python 3.9+, git, go, and gofmt are required for the full run.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile

FILES = {
    'README.md', 'code-review.md', 'diagnosing-bugs.md', 'tdd.md',
    'gophermind-architecture.md', 'gophermind-build-test.md',
    'gophermind-phaseflow.md',
}


def execute(args: list[str], cwd: Path, env: dict[str, str], timeout: int = 90):
    return subprocess.run(args, cwd=cwd, env=env, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          timeout=timeout, check=False)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, help='Write a JSON validation receipt.')
    parser.add_argument('--static-only', action='store_true',
                        help='Skip the isolated external-tool demonstrations.')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    packs = root / 'packs'
    checks: list[dict[str, object]] = []

    def check(name: str, condition: bool, detail: str = '') -> None:
        checks.append({'name': name, 'passed': bool(condition), 'detail': detail})
        print(('PASS' if condition else 'FAIL') + ': ' + name + (': ' + detail if detail else ''))

    found = {p.name for p in packs.iterdir() if p.is_file()}
    check('Exactly seven flat Markdown replacements', found == FILES)
    check('No nested prompt-pack directories', not any(p.is_dir() for p in packs.iterdir()))
    texts = {name: (packs / name).read_text(encoding='utf-8') for name in sorted(FILES)}
    check('ASCII prompt-pack text', all(t.isascii() for t in texts.values()))
    check('No YAML frontmatter in injected packs', all(not t.startswith('---\n') for t in texts.values()))
    check('Balanced Markdown code fences', all(
        len(re.findall(r'^```', t, re.MULTILINE)) % 2 == 0 for t in texts.values()))
    words = sum(len(t.split()) for t in texts.values())
    check('Prompt-pack word budget <= 3500', words <= 3500, str(words))
    check('No incomplete implementation examples', all(
        'func (t *CSVTool)' not in t and 'newMockProvider' not in t for t in texts.values()))
    check('No absent upstream workflow dependencies', all(
        x not in '\n'.join(texts.values()) for x in
        ('scripts/hitl-loop.template.sh', '[tests.md]', '[mocking.md]', '/setup-matt-pocock-skills')))
    check('Separate Standards and Spec report sections',
          '`## Standards` and `## Spec`' in texts['code-review.md'])
    check('No root-level PhaseFlow state path',
          '.planning/state.json' not in texts['gophermind-phaseflow.md'])
    check('No recursive format-write instruction',
          'gofmt -w .' not in texts['gophermind-build-test.md'])
    check('License and provenance retained',
          (root / 'mattpocock-skills-MIT.txt').is_file() and
          (root / 'references/PROVENANCE.txt').is_file())

    broken_links = []
    for page in [root / 'SKILL.md', *root.glob('references/*.md')]:
        for target in re.findall(r'\]\(([^)]+)\)', page.read_text(encoding='utf-8')):
            if '://' in target or target.startswith('#'):
                continue
            local = target.split('#', 1)[0]
            if local and not (page.parent / local).exists():
                broken_links.append(f'{page.name}: {target}')
    check('Bundled Markdown links resolve', not broken_links, '; '.join(broken_links))

    tools = {name: shutil.which(name) for name in ('git', 'go', 'gofmt')}
    versions = {}
    if not args.static_only:
        check('Full-validation tools available', all(tools.values()),
              ', '.join(f'{k}={v or "MISSING"}' for k, v in tools.items()))
        if all(tools.values()):
            env = os.environ.copy()
            env.update({'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': os.devnull,
                        'GIT_TERMINAL_PROMPT': '0', 'GOTOOLCHAIN': 'local',
                        'GOPROXY': 'off', 'GOWORK': 'off', 'GOFLAGS': ''})
            for k in ('GIT_DIR', 'GIT_WORK_TREE', 'GIT_INDEX_FILE', 'GIT_CONFIG_COUNT',
                      'GIT_CONFIG_PARAMETERS', 'GIT_OBJECT_DIRECTORY', 'GIT_COMMON_DIR',
                      'GIT_ALTERNATE_OBJECT_DIRECTORIES'):
                env.pop(k, None)
            for key in list(env):
                if key.startswith(('GIT_CONFIG_KEY_', 'GIT_CONFIG_VALUE_')):
                    env.pop(key, None)
            with tempfile.TemporaryDirectory(prefix='gophermind-pack-validation-') as temp:
                work = Path(temp)
                env['GOCACHE'] = str(work / 'go-cache')
                versions['go'] = execute([str(tools['go']), 'version'], work, env).stdout.strip()
                versions['git'] = execute([str(tools['git']), '--version'], work, env).stdout.strip()
                fmt = work / 'format-fixture'
                fmt.mkdir()
                paths = [fmt / 'first file.go', fmt / 'second.go']
                good = 'package fixture\n\nfunc Value() int { return 1 }\n'
                bad = 'package fixture\nfunc Value()int{return 1}\n'
                paths[0].write_text(bad)
                paths[1].write_text('package fixture\n')
                block = re.search(r'```sh\n(.*?)\n```', texts['gophermind-build-test.md'], re.DOTALL)
                if block is None:
                    raise ValueError('Could not locate the formatting gate to validate.')
                command = block.group(1).replace(
                    './path/to/changed.go ./path/to/another.go',
                    ' '.join(shlex.quote(str(p)) for p in paths))
                direct = execute([str(tools['gofmt']), '-l', str(paths[0])], fmt, env)
                check('Original gofmt list-only gate returns success despite differences',
                      direct.returncode == 0 and bool(direct.stdout.strip()),
                      f'exit={direct.returncode}; listed={bool(direct.stdout.strip())}')
                original_sha = hashlib.sha256(paths[0].read_bytes()).hexdigest()
                result = execute(['/bin/sh', '-c', command], fmt, env)
                check('Replacement formatting gate rejects unformatted input', result.returncode != 0,
                      f'exit={result.returncode}')
                check('Formatting verification does not modify files',
                      hashlib.sha256(paths[0].read_bytes()).hexdigest() == original_sha)
                paths[0].write_text(good)
                result = execute(['/bin/sh', '-c', command], fmt, env)
                check('Replacement formatting gate accepts formatted input and paths with spaces',
                      result.returncode == 0, f'exit={result.returncode}')
                paths[0].write_text('package fixture\nfunc (\n')
                result = execute(['/bin/sh', '-c', command], fmt, env)
                check('Replacement formatting gate rejects invalid Go syntax', result.returncode != 0,
                      f'exit={result.returncode}')
                paths[0].unlink()
                result = execute(['/bin/sh', '-c', command], fmt, env)
                check('Replacement formatting gate rejects missing files', result.returncode != 0,
                      f'exit={result.returncode}')
                paths[0].write_text(good)
                no_tools_env = env.copy()
                no_tools_env['PATH'] = str(work / 'nonexistent-bin')
                result = execute(['/bin/sh', '-c', command], fmt, no_tools_env)
                check('Replacement formatting gate rejects missing formatter', result.returncode != 0,
                      f'exit={result.returncode}')

                repo = work / 'git-fixture'
                repo.mkdir()
                git_prefix = [str(tools['git']), '-c', 'core.hooksPath=' + os.devnull,
                              '-c', 'commit.gpgsign=false', '-c', 'user.name=Pack Validator',
                              '-c', 'user.email=validator@example.invalid']

                def git(*arguments: str, must_pass: bool = True):
                    result = execute(git_prefix + list(arguments), repo, env)
                    if must_pass and result.returncode:
                        raise RuntimeError('Fixture git command failed: ' + result.stderr)
                    return result

                git('init', '-q', '-b', 'main')
                (repo / 'shared.txt').write_text('base\n')
                git('add', 'shared.txt')
                git('commit', '-qm', 'base')
                common = git('rev-parse', 'HEAD').stdout.strip()
                (repo / 'main-only.txt').write_text('advanced base branch\n')
                git('add', 'main-only.txt')
                git('commit', '-qm', 'main advances')
                git('switch', '-q', '-c', 'feature', common)
                (repo / 'feature.txt').write_text('feature\n')
                git('add', 'feature.txt')
                git('commit', '-qm', 'feature change')
                branch = set(git('diff', '--no-ext-diff', '--no-textconv', '--name-only',
                                 'main...HEAD', '--').stdout.splitlines())
                exact = set(git('diff', '--no-ext-diff', '--no-textconv', '--name-only',
                                'main', 'HEAD', '--').stdout.splitlines())
                check('Branch and exact-baseline diffs have intentionally distinct scopes',
                      branch == {'feature.txt'} and exact == {'feature.txt', 'main-only.txt'})
                (repo / 'shared.txt').write_text('staged modification\n')
                git('add', 'shared.txt')
                (repo / 'feature.txt').write_text('unstaged modification\n')
                (repo / 'untracked file.txt').write_text('new\n')
                local = set(git('diff', '--name-only', 'HEAD', '--').stdout.splitlines())
                staged = set(git('diff', '--cached', '--name-only', '--').stdout.splitlines())
                unstaged = set(git('diff', '--name-only', '--').stdout.splitlines())
                untracked = set(git('ls-files', '--others', '--exclude-standard', '-z').stdout.rstrip('\0').split('\0'))
                check('Local tracked review includes staged and unstaged net changes',
                      local == {'shared.txt', 'feature.txt'})
                check('Staged and unstaged review scopes remain distinct',
                      staged == {'shared.txt'} and unstaged == {'feature.txt'})
                check('Untracked paths are separately enumerated without word splitting',
                      untracked == {'untracked file.txt'})
                result = git('rev-parse', '--verify', '--end-of-options',
                             'does-not-exist^{commit}', must_pass=False)
                check('Invalid commit references fail before review', result.returncode != 0)

                go_dir = work / 'containment-fixture'
                go_dir.mkdir()
                (go_dir / 'go.mod').write_text('module example.invalid/containmentdemo\n\ngo 1.20\n')
                (go_dir / 'containment_test.go').write_text('''package containmentdemo

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestOriginalPrefixCheckAcceptsSibling(t *testing.T) {
    parent := t.TempDir()
    root := filepath.Join(parent, "repo")
    outside := filepath.Join(parent, "repo-other", "data.txt")
    if !strings.HasPrefix(outside, root) {
        t.Fatal("expected demonstration of unsafe prefix acceptance")
    }
}

func TestOriginalPrefixCheckAcceptsEscapingSymlink(t *testing.T) {
    parent := t.TempDir()
    root := filepath.Join(parent, "repo")
    if err := os.Mkdir(root, 0700); err != nil { t.Fatal(err) }
    outside := filepath.Join(parent, "outside.txt")
    if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil { t.Fatal(err) }
    link := filepath.Join(root, "link.txt")
    if err := os.Symlink(outside, link); err != nil { t.Fatal(err) }
    path, err := filepath.Abs(link)
    if err != nil { t.Fatal(err) }
    if !strings.HasPrefix(path, root) { t.Fatal("expected prefix acceptance") }
    data, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if string(data) != "outside" { t.Fatal("expected access outside root") }
}
''')
                result = execute([str(tools['go']), 'test', '-count=1', '-v', './...'], go_dir, env)
                check('Original string-prefix check admits sibling and symlink escapes in fixtures',
                      result.returncode == 0 and 'PASS: TestOriginalPrefixCheckAcceptsSibling' in result.stdout
                      and 'PASS: TestOriginalPrefixCheckAcceptsEscapingSymlink' in result.stdout,
                      result.stdout.strip() if result.returncode == 0 else result.stderr.strip())

    receipt = {
        'passed': all(bool(c['passed']) for c in checks),
        'check_count': len(checks), 'passed_count': sum(bool(c['passed']) for c in checks),
        'mode': 'static-only' if args.static_only else 'static-and-isolated-command-fixtures',
        'versions': versions, 'checks': checks,
        'limitations': [
            'No GopherMind repository, loader, or PhaseFlow runtime was available.',
            'No model-in-the-loop behavioral acceptance tests were run.',
            'No macOS/iOS/release or production environment was tested.',
            'Containment demonstrations do not test a replacement runtime implementation.',
        ],
    }
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
    print(f'\n{receipt["passed_count"]}/{len(checks)} checks passed.')
    return 0 if receipt['passed'] else 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        print(f'VALIDATION ERROR: {error}', file=sys.stderr)
        raise SystemExit(2)

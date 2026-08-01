#!/usr/bin/env python3
"""
sqlc is not installed here, so this checks the queries the next best way: it
rewrites sqlc.arg()/sqlc.narg() into positional parameters and asks Postgres to
PREPARE each statement. That catches typos, unknown columns, type errors and
malformed SQL — everything except sqlc's own codegen quirks.
"""
import re
import subprocess
import sys
import pathlib

FILES = ['users.sql', 'password_reset_tokens.sql', 'oauth_accounts.sql', 'words.sql', 'user_words.sql']
QUERIES_DIR = pathlib.Path('/home/claude/backend/sql/queries')

named = re.compile(r"sqlc\.(?:n?arg)\(\s*'([a-z_]+)'\s*\)")


def to_positional(sql: str) -> str:
    """Replace named args with $N, continuing after any explicit placeholders."""
    explicit = [int(n) for n in re.findall(r'\$(\d+)', sql)]
    next_index = max(explicit) + 1 if explicit else 1
    assigned: dict[str, int] = {}

    def swap(match: re.Match) -> str:
        nonlocal next_index
        name = match.group(1)
        if name not in assigned:
            assigned[name] = next_index
            next_index += 1
        return f'${assigned[name]}'

    return named.sub(swap, sql)


def split_queries(text: str):
    parts = re.split(r'^-- name:\s*(\w+)\s*:(\w+)\s*$', text, flags=re.M)
    # parts[0] is any preamble; then (name, kind, body) triples
    for i in range(1, len(parts), 3):
        yield parts[i], parts[i + 1], parts[i + 2]


def psql(sql: str):
    return subprocess.run(
        ['su', 'postgres', '-c',
         f'psql -h 127.0.0.1 -p 5433 -d appdb -v ON_ERROR_STOP=1 -q -f /tmp/_q.sql'],
        capture_output=True, text=True,
    )


failures = 0
checked = 0

for filename in FILES:
    path = QUERIES_DIR / filename
    if not path.exists():
        print(f'  ?  {filename} missing')
        continue

    print(f'\n{filename}')
    for name, kind, body in split_queries(path.read_text()):
        statement = to_positional(body).strip().rstrip(';')
        if not statement:
            continue

        pathlib.Path('/tmp/_q.sql').write_text(f'PREPARE stmt_check AS\n{statement};\nDEALLOCATE stmt_check;\n')
        result = psql(statement)
        checked += 1

        if result.returncode == 0:
            print(f'  ok    {name} ({kind})')
        else:
            failures += 1
            error = [line for line in result.stderr.splitlines() if 'ERROR' in line or 'DETAIL' in line or 'HINT' in line]
            print(f'  FAIL  {name} ({kind})')
            for line in error[:3]:
                print(f'          {line.strip()}')

print(f'\n{checked} queries checked, {failures} failed')
sys.exit(1 if failures else 0)

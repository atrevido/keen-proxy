#!/usr/bin/env python3
"""Keep literal Cyrillic in Russian documentation and localization resources only."""
from pathlib import Path
import re
import subprocess
import sys

CYRILLIC = re.compile(r'[\u0400-\u052f\u1c80-\u1c8f\u2de0-\u2dff\ua640-\ua69f]')


def allowed(path: Path) -> bool:
    """Declare the small set of intentionally Russian content locations."""
    if path.parts[:2] == ('docs', 'ru'):
        return True
    if path.parts[:3] == ('web', 'src', 'locales'):
        return path.name in ('ru.ts', 'redesign.ts') or path.name.endswith('.ru.ts')
    return path.parent == Path('locales') and path.name.endswith('.ru.yml')


def violations(root: Path, paths: list[Path]) -> list[str]:
    """Return offending line locations, skipping explicitly localized and binary files."""
    found = []
    for path in paths:
        if allowed(path) or not (root / path).is_file():
            continue
        try:
            text = (root / path).read_text(encoding='utf-8')
        except UnicodeDecodeError:
            continue
        for number, line in enumerate(text.splitlines(), 1):
            if CYRILLIC.search(line):
                found.append(f'{path.as_posix()}:{number}')
    return found


def main() -> int:
    """Scan versioned and new non-ignored files without reading local reports or secrets."""
    root = Path(__file__).resolve().parents[1]
    raw = subprocess.check_output(['git', '-C', str(root), 'ls-files', '--cached', '--others', '--exclude-standard', '-z'])
    paths = [Path(name) for name in raw.decode().split('\0') if name]
    found = violations(root, paths)
    for location in found:
        print(f'{location}: Cyrillic text outside Russian documentation/localization', file=sys.stderr)
    if not found:
        print('Source language check passed')
    return int(bool(found))


if __name__ == '__main__':
    raise SystemExit(main())

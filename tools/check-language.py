#!/usr/bin/env python3
"""Keep literal Cyrillic in Russian documentation and localization resources only."""
from pathlib import Path
import re
import subprocess
import sys

CYRILLIC = re.compile(r'[\u0400-\u052f\u1c80-\u1c8f\u2de0-\u2dff\ua640-\ua69f]')
SOURCE_MANIFEST = '.release-source-files'


def allowed(path: Path) -> bool:
    """Declare the small set of intentionally Russian content locations."""
    if path.parts[:2] == ('docs', 'ru'):
        return True
    if path.parts[:3] == ('web', 'src', 'locales'):
        return path.name in ('ru.ts', 'redesign.ts') or path.name.endswith('.ru.ts')
    return path.parent == Path('locales') and path.name.endswith('.ru.yml')


def violations(root: Path, paths: list[Path]) -> list[str]:
    """Return offending line locations, skipping explicitly localized and binary files."""
    root = root.resolve()
    found = []
    for path in paths:
        source = root / path
        if allowed(path) or not source.is_file() or not source.resolve().is_relative_to(root):
            continue
        try:
            text = source.read_text(encoding='utf-8')
        except UnicodeDecodeError:
            continue
        for number, line in enumerate(text.splitlines(), 1):
            if CYRILLIC.search(line):
                found.append(f'{path.as_posix()}:{number}')
    return found


def source_paths(root: Path) -> list[Path] | None:
    """Read the Git file set or the tracked-file list packaged with a release."""
    if (root / '.git').exists():
        raw = subprocess.check_output(['git', '-C', str(root), 'ls-files', '--cached', '--others', '--exclude-standard', '-z'])
    else:
        manifest = root / SOURCE_MANIFEST
        if not manifest.is_file():
            return None
        raw = manifest.read_bytes()
    return [Path(name) for name in raw.decode().split('\0') if name]


def main() -> int:
    """Scan source files without walking local reports or secrets."""
    root = Path(__file__).resolve().parents[1]
    paths = source_paths(root)
    if paths is None:
        print('Source language check skipped: no Git checkout or release file list')
        return 0
    found = violations(root, paths)
    for location in found:
        print(f'{location}: Cyrillic text outside Russian documentation/localization', file=sys.stderr)
    if not found:
        print('Source language check passed')
    return int(bool(found))


if __name__ == '__main__':
    raise SystemExit(main())

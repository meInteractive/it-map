#!/usr/bin/env python3
"""Проверка локальных Markdown-ссылок и ссылок на файлы текущей main в GitHub."""
import re
import subprocess
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
INLINE = re.compile(r"(?<!\\)\]\((<[^>]+>|[^\s)]+)(?:\s+['\"][^\n]*?['\"])?\)")
REFERENCE = re.compile(r"^ {0,3}\[[^\]\n]+\]:\s*(<[^>]+>|[^\s]+)")
PREFIXES = (
    "https://github.com/meInteractive/it-map/blob/main/",
    "https://github.com/meInteractive/it-map/tree/main/",
    "https://raw.githubusercontent.com/meInteractive/it-map/main/",
)


def main():
    names = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=ROOT
    ).decode().split("\0")
    checked, errors = 0, []
    for name in sorted(set(names)):
        source = ROOT / name
        if source.suffix.lower() != ".md" or not source.is_file():
            continue
        fence = None
        for number, line in enumerate(source.read_text().splitlines(), 1):
            marker = re.match(r"^\s*(`{3,}|~{3,})", line)
            if marker:
                if fence is None:
                    fence = marker[1][0]
                elif marker[1][0] == fence:
                    fence = None
                continue
            if fence:
                continue
            matches = [m[1] for m in INLINE.finditer(line)]
            reference = REFERENCE.match(line)
            if reference:
                matches.append(reference[1])
            for raw in matches:
                target = raw[1:-1] if raw.startswith("<") else raw
                prefix = next((p for p in PREFIXES if target.startswith(p)), None)
                parsed = urlsplit(target[len(prefix):] if prefix else target)
                if parsed.scheme or parsed.netloc or not parsed.path or parsed.path.startswith("/"):
                    continue
                base = ROOT if prefix else source.parent
                path = (base / unquote(parsed.path)).resolve()
                checked += 1
                if not path.is_relative_to(ROOT) or not path.exists():
                    errors.append(f"{name}:{number}: {raw}")
    if errors:
        print("Не найдены цели ссылок:\n" + "\n".join(errors))
        return 1
    print(f"Проверено локальных ссылок: {checked}; отсутствующих файлов нет.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

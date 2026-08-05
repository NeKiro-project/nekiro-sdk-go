#!/usr/bin/env python3
"""Build the bilingual MkDocs source tree from the SDK's canonical Markdown."""

from __future__ import annotations

import argparse
import posixpath
import re
import shutil
import sys
import urllib.parse
from pathlib import Path
from typing import NoReturn


REPO_ROOT = Path(__file__).resolve().parents[1]
WIKI_ROOT = REPO_ROOT / "repowiki"
DEFAULT_OUTPUT = REPO_ROOT / ".repowiki-site"
SOURCE_URL_PREFIX = "https://github.com/NeKiro-project/nekiro-sdk-go/blob/main/"
MARKDOWN_LINK = re.compile(r"(?<!!)(\[[^\]]+\])\(([^)]+)\)")
H1 = re.compile(r"^#\s+(.+?)\s*$")
SOURCES = (
    ("README.md", "source-docs/overview.md"),
    ("agent/README.md", "source-docs/agent.md"),
    ("client/README.md", "source-docs/client.md"),
)
SOURCE_MAP = dict(SOURCES)


def fail(message: str) -> NoReturn:
    raise ValueError(message)


def document_title(path: Path) -> str:
    for line in path.read_text(encoding="utf-8").splitlines():
        match = H1.match(line)
        if match:
            return match.group(1).strip()
    fail(f"source document has no level-one heading: {path.relative_to(REPO_ROOT)}")


def source_url(source: str) -> str:
    return SOURCE_URL_PREFIX + urllib.parse.quote(source, safe="/")


def rewrite_links(source: str, target: str, text: str) -> str:
    source_posix = source.replace("\\", "/")

    def replace(match: re.Match[str]) -> str:
        destination = match.group(2).strip()
        if destination.startswith(("http://", "https://", "mailto:", "#", "<")):
            return match.group(0)
        parts = destination.split(None, 1)
        link_target = parts[0]
        suffix = f" {parts[1]}" if len(parts) == 2 else ""
        fragment = ""
        if "#" in link_target:
            link_target, raw_fragment = link_target.split("#", 1)
            fragment = f"#{raw_fragment}"
        if not link_target.endswith(".md"):
            return match.group(0)
        resolved = posixpath.normpath(
            posixpath.join(posixpath.dirname(source_posix), link_target)
        )
        if resolved in SOURCE_MAP:
            mirrored = SOURCE_MAP[resolved]
            relative = posixpath.relpath(mirrored, posixpath.dirname(target))
            link = urllib.parse.quote(relative, safe="/")
        else:
            link = source_url(resolved)
        return f"{match.group(1)}({link}{fragment}{suffix})"

    return MARKDOWN_LINK.sub(replace, text)


def source_page(source: str, target: str, language: str) -> str:
    path = REPO_ROOT / source
    if language == "zh":
        note = (
            '<div class="source-note">Canonical source：'
            f'<a href="{source_url(source)}"><code>{source}</code></a>。'
            "本页保留英文 canonical 正文，中文导航和入口已提供。</div>"
        )
    else:
        note = (
            '<div class="source-note">Canonical source: '
            f'<a href="{source_url(source)}"><code>{source}</code></a>. '
            "This page is rendered from the repository document during the MkDocs build.</div>"
        )
    body = rewrite_links(source, target, path.read_text(encoding="utf-8"))
    return f"{note}\n\n{body}"


def source_index(language: str) -> str:
    if language == "zh":
        lines = [
            "# 源文档",
            "",
            "以下页面从 SDK 仓库内的 canonical Markdown 文档生成。",
            "中文入口已提供；正文在完成审阅翻译前保留英文规范文本。",
            "",
        ]
    else:
        lines = [
            "# Source documentation",
            "",
            "These pages are rendered from the canonical Markdown documents in this repository.",
            "Edit the source README files rather than the generated MkDocs tree.",
            "",
        ]
    for source, target in SOURCES:
        title = document_title(REPO_ROOT / source)
        link = posixpath.relpath(target, "source-docs")
        lines.append(f"- [{title}]({urllib.parse.quote(link, safe='/')})")
    return "\n".join(lines) + "\n"


def copy_tracked_wiki(output: Path) -> None:
    for path in WIKI_ROOT.rglob("*"):
        if path.is_dir():
            continue
        relative = path.relative_to(WIKI_ROOT)
        if relative.parts[0] == "zh":
            continue
        if relative.parts[0] == "assets":
            destination = output / relative
        elif path.suffix == ".md":
            destination = output / "en" / relative
        else:
            fail(f"unsupported tracked RepoWiki file: {relative}")
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, destination)

    for path in (WIKI_ROOT / "zh").rglob("*"):
        if path.is_dir():
            continue
        relative = path.relative_to(WIKI_ROOT)
        destination = output / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, destination)


def validate() -> None:
    for relative in ("index.md", "zh/index.md", "assets/stylesheets/extra.css"):
        if not (WIKI_ROOT / relative).is_file():
            fail(f"missing RepoWiki input: repowiki/{relative}")
    titles: set[str] = set()
    for source, target in SOURCES:
        path = REPO_ROOT / source
        if not path.is_file():
            fail(f"missing source document: {source}")
        title = document_title(path)
        if title in titles:
            fail(f"duplicate source document title: {title}")
        titles.add(title)
        rewrite_links(source, target, path.read_text(encoding="utf-8"))
    for path in WIKI_ROOT.rglob("*.md"):
        text = path.read_text(encoding="utf-8")
        if "{{" in text or "relative_url" in text:
            fail(f"Jekyll/Liquid link remains in RepoWiki source: {path.relative_to(REPO_ROOT)}")


def build(output: Path) -> None:
    if output.exists():
        if output.is_dir():
            shutil.rmtree(output)
        else:
            output.unlink()
    output.mkdir(parents=True, exist_ok=True)
    copy_tracked_wiki(output)
    for language in ("en", "zh"):
        generated_root = output / language / "source-docs"
        generated_root.mkdir(parents=True, exist_ok=True)
        (generated_root / "index.md").write_text(source_index(language), encoding="utf-8")
        for source, target in SOURCES:
            destination = output / language / target
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_text(source_page(source, target, language), encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    parser.add_argument("--check", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        validate()
        if args.check:
            print(f"RepoWiki check passed: {len(SOURCES)} source documents, 2 locales")
        else:
            output = args.output if args.output.is_absolute() else REPO_ROOT / args.output
            build(output)
            print(f"MkDocs source generated: {output}")
    except ValueError as error:
        print(f"RepoWiki build failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

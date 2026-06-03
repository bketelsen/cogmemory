#!/usr/bin/env python3
"""Curated importer: Chapterhouse wiki corpus -> cogmemory wiki/ tier.

Migrates the retired ~/.chapterhouse/wiki corpus into a cogmemory store's
wiki/ tier, applying the normalization rules from docs/WIKI-TIER.md. Curated,
not lift-and-shift: conversations + the meta action-log are glaciered; stale
index/taxonomy/redirect/autostub pages are discarded.

Idempotent: clears and recreates DEST/wiki/ and the glacier wiki-* slabs on
each run. Safe to re-run.

Usage:
    import_wiki.py [SOURCE] [DEST]
    SOURCE default: ~/.chapterhouse/wiki
    DEST   default: ~/.local/share/cogmemory-test/memory   (the TEST store)
"""
import os, sys, re, shutil, datetime, glob

try:
    import yaml
except ImportError:
    sys.exit("PyYAML required (pip install pyyaml)")

HOME = os.path.expanduser("~")
SOURCE = os.path.abspath(sys.argv[1]) if len(sys.argv) > 1 else os.path.join(HOME, ".chapterhouse/wiki")
DEST   = os.path.abspath(sys.argv[2]) if len(sys.argv) > 2 else os.path.join(HOME, ".local/share/cogmemory-test/memory")

PAGES = os.path.join(SOURCE, "pages")
WIKI_DEST = os.path.join(DEST, "wiki")
GLACIER_DEST = os.path.join(DEST, "glacier", "chapterhouse")

ENTITY_CATEGORIES = ["topics", "tools", "projects", "research", "ideas", "people"]
STATUS_MAP = {"seed": "draft", "germinating": "active", "mature": "active"}
VALID_STATUS = {"draft", "active", "archived", "superseded"}
DROP_KEYS = {"version", "autostub", "last_updated"}
KEEP_KEYS = ["title", "summary", "updated", "entity_type", "status", "tags",
             "related", "confidence", "contested", "contradictions"]

log = {"imported": [], "glaciered": [], "discarded": [], "synthesized_fm": [],
       "related_unnormalized": [], "related_rewritten": 0}


def split_frontmatter(text):
    """Return (fm_dict_or_None, body_str). Tolerant of a leading HTML comment."""
    lines = text.split("\n")
    i = 0
    # skip a leading BOM/comment line like the daemon does
    while i < len(lines) and (lines[i].startswith("\ufeff") or lines[i].lstrip().startswith("<!--")):
        i += 1
    if i >= len(lines) or lines[i].strip() != "---":
        return None, text
    j = i + 1
    fm_lines = []
    while j < len(lines) and lines[j].strip() != "---":
        fm_lines.append(lines[j]); j += 1
    if j >= len(lines):
        return None, text  # unterminated
    body = "\n".join(lines[j + 1:])
    block = "\n".join(fm_lines)
    try:
        fm = yaml.safe_load(block) or {}
        if not isinstance(fm, dict):
            fm = _tolerant_fm(fm_lines)
    except yaml.YAMLError:
        # Strict parse failed (commonly an unquoted scalar with a colon, e.g.
        # "summary: Two CLIs: mentat ..."). Recover field-by-field rather than
        # discard real content; the writer re-emits via safe_dump (quoted).
        fm = _tolerant_fm(fm_lines)
    return fm, body


# Keys whose values are plain scalars we can recover line-by-line. List/flow
# values ([...]) are handled by re-parsing the single line via yaml.
_SCALAR_KEYS = ("title", "summary", "updated", "entity_type", "status",
                "last_updated", "version", "confidence", "contested",
                "contradictions", "autostub")


def _tolerant_fm(fm_lines):
    """Best-effort frontmatter recovery for blocks that fail strict YAML.

    NOTE: multi-line block scalars (`key: |` / `key: >`) and nested mappings
    are NOT preserved by this flat line-parser — acceptable for this corpus
    (flat scalar frontmatter + flow-list related:/tags:). Do not rely on it for
    richer frontmatter without extending the parser.
    Splits each top-level `key: value` line; for list/flow values, re-parses
    just that line; for scalars, takes the raw remainder verbatim (the writer
    re-quotes on output)."""
    out = {}
    for line in fm_lines:
        if not line or line[0] in (" ", "\t", "#"):
            continue
        if ":" not in line:
            continue
        key, _, val = line.partition(":")
        key = key.strip()
        val = val.strip()
        if not key:
            continue
        if val.startswith("[") or val.startswith("{"):
            try:
                out[key] = yaml.safe_load(key + ": " + val)[key]
                continue
            except Exception:
                pass
        # strip matching surrounding quotes if present
        if len(val) >= 2 and val[0] == val[-1] and val[0] in ("'", '"'):
            val = val[1:-1]
        out[key] = val
    return out


def normalize_related(entry):
    """pages/<cat>/<slug>/index.md -> wiki/<cat>/<slug>;
       pages/<cat>/<slug>/<facet>.md -> wiki/<cat>/<slug>/<facet>;
       already-wiki/... -> passthrough. Returns (normalized, ok)."""
    e = entry.strip()
    if e.startswith("wiki/"):
        return e.rstrip("/"), True
    m = re.match(r"^(?:pages/)?(.+)$", e)
    if not m:
        return e, False
    p = m.group(1)
    p = re.sub(r"/index\.md$", "", p)
    p = re.sub(r"\.md$", "", p)
    cat = p.split("/", 1)[0]
    if cat in ENTITY_CATEGORIES:
        return "wiki/" + p, True
    return e, False  # can't confidently place it


def is_discard(rel, fm, body):
    """Detect stale/aspirational/redirect/autostub pages to discard."""
    base = os.path.basename(rel)
    if rel in ("index.md", "facts.md"):
        return True, "loose root file (stale/deferred)"
    if rel.startswith("_meta/taxonomy.md"):
        return True, "old tag registry"
    if fm and fm.get("autostub"):
        return True, "autostub page"
    bl = body.lower()
    title = (fm or {}).get("title", "").lower()
    summ = (fm or {}).get("summary", "").lower()
    if "moved to" in bl[:400] or "(moved)" in title or "moved to pages/" in summ:
        return True, "moved-page redirect stub"
    if summ.startswith("ingested source:") and len(body.strip()) < 400:
        return True, "one-line ingested-source autostub"
    return False, ""


def yaml_safe_fm(fm):
    """Emit frontmatter via safe_dump so colons/specials in scalars are quoted."""
    ordered = {k: fm[k] for k in KEEP_KEYS if k in fm and fm[k] not in (None, "")}
    dumped = yaml.safe_dump(ordered, sort_keys=False, allow_unicode=True,
                            default_flow_style=False, width=10**9).rstrip("\n")
    return "---\n" + dumped + "\n---\n"


def import_page(src_abs, rel_in_pages):
    parts = rel_in_pages.split("/")
    category = parts[0]
    with open(src_abs, encoding="utf-8") as f:
        text = f.read()
    fm, body = split_frontmatter(text)
    if fm is None:
        # synthesize minimal frontmatter (e.g. agent-memory-guide.md, handoff.md)
        m = re.search(r"^#\s+(.+)$", body, re.M)
        title = m.group(1).strip() if m else os.path.splitext(os.path.basename(rel_in_pages))[0]
        mtime = datetime.date.fromtimestamp(os.path.getmtime(src_abs)).isoformat()
        fm = {"title": title, "entity_type": category, "status": "active",
              "updated": mtime, "tags": []}
        log["synthesized_fm"].append(rel_in_pages)

    disc, why = is_discard(rel_in_pages, fm, body)
    if disc:
        log["discarded"].append((rel_in_pages, why)); return

    # --- normalize frontmatter ---
    for k in DROP_KEYS:
        fm.pop(k, None)
    # entity_type = directory wins (facet law / GAP-03)
    fm["entity_type"] = category
    # status mapping + default
    st = str(fm.get("status", "")).strip().lower()
    st = STATUS_MAP.get(st, st)
    fm["status"] = st if st in VALID_STATUS else "active"
    # updated default from mtime if missing
    if not fm.get("updated"):
        fm["updated"] = datetime.date.fromtimestamp(os.path.getmtime(src_abs)).isoformat()
    # updated -> YYYY-MM-DD string (yaml may have parsed a date)
    fm["updated"] = str(fm["updated"])[:10]
    if "title" not in fm or not fm["title"]:
        m = re.search(r"^#\s+(.+)$", body, re.M)
        fm["title"] = m.group(1).strip() if m else category
    # related normalization
    rel = fm.get("related")
    if isinstance(rel, list):
        out = []
        for r in rel:
            n, ok = normalize_related(str(r))
            if ok and n != str(r):
                log["related_rewritten"] += 1
            if not ok:
                log["related_unnormalized"].append((rel_in_pages, str(r)))
            out.append(n)
        fm["related"] = out
    elif rel:
        fm.pop("related", None)

    # body link rewrite: wiki:pages/x -> wiki/x ; [[pages/x]] left if ambiguous
    body = body.replace("wiki:pages/", "wiki/").replace("[[pages/", "[[wiki/")

    out_rel = os.path.join("wiki", rel_in_pages)
    out_abs = os.path.join(DEST, out_rel)
    os.makedirs(os.path.dirname(out_abs), exist_ok=True)
    with open(out_abs, "w", encoding="utf-8") as f:
        f.write(yaml_safe_fm(fm) + body.lstrip("\n"))
    log["imported"].append((out_rel, category))


def glacier_conversations():
    conv = os.path.join(PAGES, "conversations")
    if not os.path.isdir(conv): return
    dest = os.path.join(GLACIER_DEST, "wiki-conversations")
    os.makedirs(dest, exist_ok=True)
    files = sorted(glob.glob(os.path.join(conv, "*.md")))
    dates = []
    for fp in files:
        b = os.path.basename(fp)
        shutil.copy2(fp, os.path.join(dest, b))
        m = re.search(r"(\d{4}-\d{2}-\d{2})", b)
        if m: dates.append(m.group(1))
        log["glaciered"].append("glacier/chapterhouse/wiki-conversations/" + b)
    dr = f"{min(dates)} to {max(dates)}" if dates else ""
    slab = {"type": "wiki-conversations", "domain": "chapterhouse",
            "date_range": dr, "entries": len(files),
            "summary": "Imported Chapterhouse wiki conversation logs (chat dumps), archived on wiki-tier import."}
    with open(os.path.join(dest, "index.md"), "w", encoding="utf-8") as f:
        f.write("---\n" + yaml.safe_dump(slab, sort_keys=False, allow_unicode=True, width=10**9).rstrip() +
                "\n---\n# Chapterhouse Wiki Conversations (archived)\n")


def glacier_meta_log():
    src = os.path.join(PAGES, "_meta", "log.md")
    if not os.path.isfile(src): return
    os.makedirs(GLACIER_DEST, exist_ok=True)
    with open(src, encoding="utf-8") as f:
        body = f.read()
    slab = {"type": "wiki-action-log", "domain": "chapterhouse",
            "summary": "Chapterhouse wiki action log (mutation history), archived on wiki-tier import."}
    out = os.path.join(GLACIER_DEST, "wiki-meta-log.md")
    with open(out, "w", encoding="utf-8") as f:
        f.write("---\n" + yaml.safe_dump(slab, sort_keys=False, allow_unicode=True, width=10**9).rstrip() +
                "\n---\n" + body)
    log["glaciered"].append("glacier/chapterhouse/wiki-meta-log.md")


def main():
    if not os.path.isdir(PAGES):
        sys.exit(f"source pages dir not found: {PAGES}")
    # Safety guard: refuse to run destructive rmtree if DEST is unset, root, or
    # not an existing directory we can resolve. Protects against a typo'd CLI
    # arg (e.g. `import_wiki.py SRC /` -> rmtree('/wiki')). The rmtree targets
    # below are all os.path.join(DEST, ...) — this asserts DEST itself is sane.
    real_dest = os.path.realpath(DEST)
    if real_dest in ("/", os.path.realpath(os.path.expanduser("~"))) or real_dest.count(os.sep) < 2:
        sys.exit(f"refusing to operate on unsafe DEST: {DEST}")
    if not os.path.isdir(real_dest):
        sys.exit(f"DEST is not an existing directory: {DEST}")
    # idempotent: clear prior wiki tier + glacier slabs
    if os.path.isdir(WIKI_DEST): shutil.rmtree(WIKI_DEST)
    for p in [os.path.join(GLACIER_DEST, "wiki-conversations"),
              os.path.join(GLACIER_DEST, "wiki-meta-log.md")]:
        if os.path.isdir(p): shutil.rmtree(p)
        elif os.path.isfile(p): os.remove(p)
    os.makedirs(WIKI_DEST, exist_ok=True)

    for cat in ENTITY_CATEGORIES:
        cdir = os.path.join(PAGES, cat)
        if not os.path.isdir(cdir): continue
        for abs_path in glob.glob(os.path.join(cdir, "**", "*.md"), recursive=True):
            rel = os.path.relpath(abs_path, PAGES)
            import_page(abs_path, rel)

    glacier_conversations()
    glacier_meta_log()

    # --- report ---
    from collections import Counter
    by_cat = Counter(c for _, c in log["imported"])
    print(f"SOURCE={SOURCE}\nDEST={DEST}\n")
    print("=== imported by category ===")
    for c in ENTITY_CATEGORIES:
        print(f"  {c:10s} {by_cat.get(c,0)}")
    print(f"  {'TOTAL':10s} {len(log['imported'])}")
    print(f"\nsynthesized frontmatter: {len(log['synthesized_fm'])} {log['synthesized_fm']}")
    print(f"related: entries rewritten: {log['related_rewritten']}")
    print(f"related: entries NOT normalized: {len(log['related_unnormalized'])}")
    for p, r in log["related_unnormalized"]:
        print(f"    {p}: {r}")
    print(f"\nglaciered: {len(log['glaciered'])} files")
    print(f"discarded: {len(log['discarded'])}")
    for p, why in log["discarded"]:
        print(f"    {p}  ({why})")


if __name__ == "__main__":
    main()

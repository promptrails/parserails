#!/usr/bin/env bash
# Convert a corpus of documents to Markdown with ParseRails, so a public
# doc→Markdown benchmark can score the output with its own evaluator.
#
#   ./run.sh <corpus-dir> <output-dir> [--ocr tesseract|http] [--ocr-url URL]
#
# Speed is measured here; quality is not. Scoring belongs to the benchmark
# that owns the ground truth — see README.md for which ones and how.
set -euo pipefail

if [[ $# -lt 2 ]]; then
	sed -n '2,9p' "$0" >&2
	exit 2
fi

corpus=$1
outdir=$2
shift 2

parserails=${PARSERAILS_BIN:-parserails}
if ! command -v "$parserails" >/dev/null 2>&1; then
	echo "building parserails from this working tree" >&2
	parserails=$(mktemp -d)/parserails
	go build -o "$parserails" ../../cmd/parserails
fi

mkdir -p "$outdir"
start=$(date +%s)
# Flags before the positional arguments: Go's flag package stops parsing at
# the first non-flag argument, so anything after them is silently ignored.
"$parserails" batch --format markdown -q "$@" "$corpus" "$outdir"
end=$(date +%s)

documents=$(find "$corpus" -type f -name '*.pdf' | wc -l | tr -d ' ')
echo "converted ${documents} document(s) in $((end - start))s → ${outdir}"

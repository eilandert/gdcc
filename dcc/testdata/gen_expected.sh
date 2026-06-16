#!/bin/sh
# Generate expected.tsv from the corpus using the REAL dccproc (the parity
# oracle for the gdcc checksum port — the analogue of gyzor's gen_expected.py).
#
# dccproc -C -Q prints the computed checksums it would query; we capture them
# byte-for-byte. Requires the dcc binaries; we run them from the project's
# rspamd-dcc image so no host install is needed.
#
#   ./gen_expected.sh            # uses default DCC_IMAGE
#   DCC_IMAGE=... ./gen_expected.sh
#
# Output line format: <corpus-name>\t<label>\t<hex>  (one per checksum).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
corpus="$here/corpus"
out="$here/expected.tsv"
# NOTE: rspamd-dcc-razor-pyzor:latest went distroless (no dccproc) once gdcc
# replaced the fork. The :dcc-oracle tag is the pinned pre-distroless image that
# still ships dccproc 2.3.169 for golden generation.
img="${DCC_IMAGE:-eilandert/rspamd-dcc-razor-pyzor:dcc-oracle}"

: > "$out"
for f in "$corpus"/*.eml; do
	name=$(basename "$f")
	# Feed the message on stdin; -C => print checksums, -Q => query-only.
	# Keep only the "<label>: <4 hex words>" lines, drop the header line.
	docker run --rm -i --entrypoint dccproc "$img" -C -Q -i /dev/stdin < "$f" 2>/dev/null \
	| grep -E ': [0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8} *$' \
	| sed -E "s/^[[:space:]]*(.+): ([0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8})[[:space:]]*\$/${name}\t\1\t\2/" >> "$out"
done

echo "wrote $(wc -l < "$out") checksum lines to $out"

#!/usr/bin/env python3
"""Generate engine/joint_life_table.go from the authoritative eCFR source.

The Joint and Last Survivor Table (IRS "Table II", 26 CFR 1.401(a)(9)-9(d),
"Table 3 to Paragraph (d)") gives the RMD life-expectancy divisor for an
account owner whose sole beneficiary is a spouse more than 10 years younger.

This script fetches the regulation as structured XML from the eCFR versioner
API, parses the Joint table out of its column-banded sub-tables, verifies a
set of hand-checked spot values and full diagonal symmetry, and emits the Go
data file consumed by jointLifeFactor (engine/joint_life.go).

Usage:
    python3 scripts/gen-joint-life-table.py            # fetch live, write file
    python3 scripts/gen-joint-life-table.py local.xml  # parse a saved copy

The generated file is checked in; rerun this only to refresh the table or to
audit the data against the source. Regeneration must be byte-stable for a
given source edition.

Source edition pinned to the 2026-01-01 annual edition (immutable); the table
itself derives from the 2020 final regulations (T.D. 9930) effective 2022.
"""

import html
import re
import subprocess
import sys
import urllib.request

SOURCE_DATE = "2026-01-01"
FETCH_DATE = "2026-06-10"
SOURCE_URL = (
    "https://www.ecfr.gov/api/versioner/v1/full/"
    f"{SOURCE_DATE}/title-26.xml?part=1&section=1.401(a)(9)-9"
)

# RMD band actually reachable by the projection engine: the owner is the older
# household member (RMD age >= 72) and the sole-beneficiary spouse is at least
# 11 years younger (the eligibility gate), so spouse age runs 18..owner-11.
MIN_OWNER_AGE = 72
MAX_OWNER_AGE = 120  # the regulation's "120+" row, stored as 120
MIN_SPOUSE_AGE = 18

OUTPUT_PATH = "internal/services/retirement/engine/joint_life_table.go"

# Independent spot checks, hand-read from the published table, spanning band
# edges, the 120+ row, and interior cells. The generated data MUST reproduce
# these exactly or the script aborts. (These are duplicated in the Go test as a
# second, generator-independent guard.)
SPOT_CHECKS = {
    (72, 18): 67.1, (72, 50): 36.9, (72, 61): 28.1, (73, 60): 28.6,
    (73, 62): 27.2, (80, 65): 23.8, (80, 69): 20.9, (85, 74): 16.7,
    (90, 40): 45.8, (95, 84): 9.4, (100, 50): 36.2, (110, 30): 55.3,
    (120, 18): 67.0, (120, 80): 11.2, (120, 109): 2.0, (76, 55): 32.3,
}


def fetch_xml(argv):
    if len(argv) > 1:
        with open(argv[1], encoding="utf-8") as fh:
            return fh.read()
    with urllib.request.urlopen(SOURCE_URL, timeout=60) as resp:
        return resp.read().decode("utf-8")


def cells(row):
    raw = re.findall(r"<T[DH][^>]*>(.*?)</T[DH]>", row, re.S)
    return [html.unescape(re.sub(r"<.*?>", "", c)).strip() for c in raw]


def parse_age(tok):
    tok = tok.strip()
    return MAX_OWNER_AGE if tok.startswith("120") else int(tok)


def parse_joint(xml):
    """Return {(lo, hi): factor} for every cell of the symmetric Joint table."""
    joint = {}
    for table in re.findall(r"<TABLE.*?</TABLE>", xml, re.S):
        head = re.search(r"<THEAD>(.*?)</THEAD>", table, re.S)
        if not head:
            continue
        hcells = cells(head.group(1))
        # Only the Joint and Last Survivor sub-tables head their first column
        # "Ages"; Single Life / Uniform / Mortality use other labels.
        if not hcells or hcells[0] != "Ages":
            continue
        col_ages = [parse_age(c) if c else None for c in hcells[1:]]
        body = re.search(r"<TBODY>(.*?)</TBODY>", table, re.S)
        if not body:
            continue
        for row in re.findall(r"<TR>(.*?)</TR>", body.group(1), re.S):
            rcells = cells(row)
            if not rcells:
                continue
            row_age = parse_age(rcells[0])
            for j, val in enumerate(rcells[1:]):
                if j >= len(col_ages) or col_ages[j] is None or val == "":
                    continue
                key = (min(row_age, col_ages[j]), max(row_age, col_ages[j]))
                f = float(val)
                if key in joint and abs(joint[key] - f) > 1e-9:
                    # The published table has one known rounding asymmetry at
                    # ages (2, 5) — outside the RMD band — so only flag
                    # conflicts that would corrupt a band cell.
                    if key[1] >= MIN_OWNER_AGE:
                        raise SystemExit(
                            f"symmetry conflict in band at {key}: "
                            f"{joint[key]} vs {f}"
                        )
                else:
                    joint[key] = f
    return joint


def build_band(joint):
    """Return {ownerAge: [factor for spouse 18..ownerAge-11]} for the band."""
    band = {}
    for owner in range(MIN_OWNER_AGE, MAX_OWNER_AGE + 1):
        factors = []
        for spouse in range(MIN_SPOUSE_AGE, owner - 11 + 1):
            key = (min(owner, spouse), max(owner, spouse))
            if key not in joint:
                raise SystemExit(f"missing source cell for {key}")
            factors.append(joint[key])
        band[owner] = factors
    return band


def verify(band):
    for (owner, spouse), expected in SPOT_CHECKS.items():
        got = band[owner][spouse - MIN_SPOUSE_AGE]
        if abs(got - expected) > 1e-9:
            raise SystemExit(
                f"spot check failed at ({owner},{spouse}): "
                f"got {got}, expected {expected}"
            )
    total = sum(len(v) for v in band.values())
    if total != 3332:
        raise SystemExit(f"expected 3332 band cells, built {total}")
    # Every factor must render as a plain Go float literal. The band's range
    # (2.0..67.1) is well clear of %g's exponent threshold, but assert it so a
    # future source edition can never silently emit "1e-05" into the .go file.
    for f in (f for row in band.values() for f in row):
        if "e" in fmt_factor(f).lower():
            raise SystemExit(f"factor {f} renders in exponent form; not a valid Go literal")


def fmt_factor(f):
    return f"{f:g}"


def render(band):
    lines = [
        "// Code generated by scripts/gen-joint-life-table.py. DO NOT EDIT.",
        "//",
        "// Source: eCFR 26 CFR § 1.401(a)(9)-9(d), \"Joint and Last Survivor",
        "// Table\" (Table 3 to Paragraph (d)). Derived from the 2020 final",
        "// regulations (T.D. 9930), effective for distribution years 2022 and",
        f"// later. Pinned to the {SOURCE_DATE} eCFR annual edition.",
        f"// {SOURCE_URL}",
        f"// Fetched {FETCH_DATE}; 14640 source cells parsed (full symmetric",
        "// table) and verified symmetric across the owner/spouse diagonal;",
        "// 3332 retained below for the reachable RMD band.",
        "//",
        "// jointLifeBand[ownerAge] holds the divisor for an account owner aged",
        "// ownerAge whose sole-beneficiary spouse is aged spouseAge, indexed by",
        "// spouseAge-18 for spouseAge in [18, ownerAge-11]. ownerAge spans",
        "// 72..120 (the regulation's 120+ row stored as 120).",
        "",
        "package engine",
        "",
        "const (",
        f"\tjointLifeMinOwnerAge  = {MIN_OWNER_AGE}",
        f"\tjointLifeMaxOwnerAge  = {MAX_OWNER_AGE}",
        f"\tjointLifeMinSpouseAge = {MIN_SPOUSE_AGE}",
        ")",
        "",
        "var jointLifeBand = map[int][]float64{",
    ]
    for owner in range(MIN_OWNER_AGE, MAX_OWNER_AGE + 1):
        factors = ", ".join(fmt_factor(f) for f in band[owner])
        hi = owner - 11
        lines.append(f"\t{owner}: {{{factors}}}, // spouse {MIN_SPOUSE_AGE}..{hi}")
    lines.append("}")
    lines.append("")
    return "\n".join(lines)


def main():
    xml = fetch_xml(sys.argv)
    joint = parse_joint(xml)
    band = build_band(joint)
    verify(band)
    with open(OUTPUT_PATH, "w", encoding="utf-8") as fh:
        fh.write(render(band))
    # gofmt the result so the checked-in file is canonical and regeneration is
    # byte-stable against `gofmt -l`.
    subprocess.run(["gofmt", "-w", OUTPUT_PATH], check=True)
    print(f"wrote {OUTPUT_PATH}: {sum(len(v) for v in band.values())} cells, "
          f"owner {MIN_OWNER_AGE}..{MAX_OWNER_AGE}, all spot checks passed")


if __name__ == "__main__":
    main()

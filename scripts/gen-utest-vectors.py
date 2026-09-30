#!/usr/bin/env python3
"""Regenerate testdata/utest_vectors.json.

The golden vectors are produced by SciPy so that benchbook's Go implementation of
the two-sided Mann-Whitney U test is checked against an independent, widely trusted
implementation rather than against itself.

    pip install scipy
    python3 scripts/gen-utest-vectors.py

Vector kinds
------------
exact      : no ties anywhere, both sides <= 50 samples -> exact rank-counting DP.
             Cross-checked against scipy.stats.mannwhitneyu(..., method="exact").
normal     : ties present and/or > 50 samples per side -> normal approximation
             with tie correction and continuity correction, cross-checked against
             scipy.stats.mannwhitneyu(..., method="asymptotic", use_continuity=True).
insufficient: sample counts below which p < alpha is unreachable; these must be
             rejected by the caller, and are kept here to pin that rule down.
"""

from __future__ import annotations

import json
import pathlib
import random

from scipy.stats import mannwhitneyu

TOLERANCE = 1e-6
SEED = 20260930


def exact_vector(name: str, a: list[float], b: list[float]) -> dict:
    """Build an exact-distribution vector (values must be tie-free)."""
    pooled = list(a) + list(b)
    if len(set(pooled)) != len(pooled):
        raise ValueError(
            f"{name}: the exact distribution assumes no ties; use a tie-free sample "
            "or generate the vector with method='asymptotic'"
        )
    result = mannwhitneyu(a, b, alternative="two-sided", method="exact")
    return {
        "name": name,
        "method": "exact",
        "a": a,
        "b": b,
        "u": float(min(result.statistic, len(a) * len(b) - result.statistic)),
        "p": float(result.pvalue),
    }


def normal_vector(name: str, a: list[float], b: list[float]) -> dict:
    """Build a normal-approximation vector, scipy-side tie + continuity corrected."""
    result = mannwhitneyu(a, b, alternative="two-sided", method="asymptotic", use_continuity=True)
    return {
        "name": name,
        "method": "normal",
        "a": a,
        "b": b,
        "u": float(min(result.statistic, len(a) * len(b) - result.statistic)),
        "p": float(result.pvalue),
    }


def main() -> None:
    rng = random.Random(SEED)
    vectors: list[dict] = []

    # Hand-written anchors, easy to eyeball.
    vectors.append(exact_vector("exact_n4_vs_n4_separated", [1, 2, 3, 4], [5, 6, 7, 8]))
    vectors.append(exact_vector("exact_n5_vs_n5_interleaved", [1, 3, 5, 7, 9], [2, 4, 6, 8, 10]))
    vectors.append(exact_vector("exact_n6_vs_n6_interleaved", [3, 11, 19, 27, 35, 43], [7, 15, 23, 31, 39, 47]))

    # Randomised tie-free pairs of every size class we care about.
    for n_a, n_b in [(4, 4), (5, 7), (8, 8), (10, 6), (12, 12), (20, 9), (30, 30), (50, 50)]:
        pool: list[float] = []
        while len(pool) < n_a + n_b:
            candidate = float(rng.randint(1, 10_000))
            if candidate not in pool:
                pool.append(candidate)
        vectors.append(exact_vector(f"exact_n{n_a}_vs_n{n_b}_random", sorted(pool[:n_a]), sorted(pool[n_a:])))

    # Ties force the normal path even at small sample counts.
    vectors.append(normal_vector("normal_identical_with_ties", [2, 4, 6, 8, 10, 12], [2, 4, 6, 8, 10, 12]))
    vectors.append(normal_vector("normal_many_ties_shifted", [1, 1, 2, 2, 3, 3, 4, 4], [2, 2, 3, 3, 4, 4, 5, 5]))
    # These two deliberately contain repeated values. Ties are what makes an
    # implementation take the normal-approximation path even at small sample counts,
    # so a tie-free "normal" vector would only prove that the exact path is faster to
    # reach. scipy's asymptotic method is the reference here.
    vectors.append(
        normal_vector(
            "normal_benchmark_like",
            [15276.0, 15276.0, 15102.0, 15330.0, 15288.0, 15276.0, 15188.0, 15250.0],
            [12480.0, 12480.0, 12399.0, 12640.0, 12471.0, 12480.0, 12488.0, 12433.0],
        )
    )
    vectors.append(
        normal_vector(
            "normal_throughput_metric",
            [512.3, 512.3, 515.1, 511.0, 513.4, 510.2],
            [698.4, 698.4, 695.5, 700.0, 699.2, 701.6],
        )
    )

    # Above the exact-cutoff sample count, the normal path takes over.
    big_a = sorted(round(rng.uniform(100, 200), 3) for _ in range(60))
    big_b = sorted(round(rng.uniform(130, 240), 3) for _ in range(60))
    vectors.append(normal_vector("normal_n60_vs_n60_random", big_a, big_b))

    payload = {
        "schema_version": 1,
        "generator": "scripts/gen-utest-vectors.py",
        "produced_by": "scipy.stats.mannwhitneyu",
        "tolerance": TOLERANCE,
        "vectors": vectors,
    }

    target = pathlib.Path(__file__).resolve().parent.parent / "testdata" / "utest_vectors.json"
    target.write_text(json.dumps(payload, indent=2, sort_keys=False) + "\n", encoding="utf-8")
    print(f"wrote {len(vectors)} vectors to {target}")


if __name__ == "__main__":
    main()

# Plan — ID digit widths supported without special libraries

    Status: decided
    Date:   2026-08-01
    Scope:  spec-file-format.md §3.3.2, every implementation
    Items:  T-0119 (this spike), T-0120 (spec change), T-0116 (validators)

What `id_width` values every implementation can honor using only native
integer machinery — no third-party bignum library — and the shared cap the
format's readers agree on.

Specs are normative. Where this document appears to contradict one, the spec
wins; T-0120 lands the change this document decides.

---

## 1. The question

T-0113 made `id_width` "one or more ASCII digits" (3–6 RECOMMENDED, warn
outside) and generalised the counter-space cap to `10^W − 1`, with no upper
bound on W. Rule 3 says a reader that cannot honor the declared grammar MUST
refuse loudly, never silently misparse. So: at what width does each language's
plain machinery stop being exact, and what one number can every reader agree on?

T-0116 found the divergence that prompted this: Go rejects a 20-digit
`id_width` (`strconv.Atoi` overflows int64 → a format violation) while the
reference checker's mawk accepts it and then demands 20-digit IDs. The two
validators disagree exactly where the declared width outruns the checker's
native integer.

## 2. The evidence (measured, 2026-08-01)

| Reader | Native exact range | Beyond it |
|---|---|---|
| Go (plain `int`/`int64`) | 18 digits (`10^18−1 < 2^63−1`) | `Atoi` errors at 19+; `Cap()` overflows int64 and silently wraps negative at width 19 |
| Java (`long`) | 18 digits | `Long.parseLong` throws; `java.math.BigInteger` (JDK, not third-party) is exact at any width |
| JavaScript (`number`) | 15 digits (`10^15−1 < 2^53−1`) | `Number("9999999999999999")` rounds silently to `1e16`; `BigInt` is built in (ES2020), exact at any width |
| check.sh (mawk double) | 15 digits | `"9999999999999999" + 0 == 10000000000000000` — the I2 comparison is wrong at 16 digits |
| Python (`int`) | unlimited | arbitrary precision natively |
| Clojure (`clojure.lang.BigInt`) | unlimited | arbitrary precision natively; `+` auto-promotes on overflow |

Two measured failures are the whole point:

- `9999999999999999` (16 nines) becomes `1e16` in **both** mawk and JS
  `number` — a silent misparse of the kind rule 3 forbids.
- Go's `Cap()` (`10^W − 1`, the loop in `mm/grammar.go`) overflows int64 at
  width 19 and returns a negative cap without an error.

`2^53 = 9007199254740992`, so every integer with ≤ 15 digits is exact in every
reader's plain arithmetic; at 16 digits some values exceed `2^53` and the
double-based readers (mawk, JS `number`) round.

## 3. Decisions

1. **"Without special libraries" means no third-party dependency.** Anything in
   the language proper or its standard library counts as native: JS `BigInt`,
   `java.math.BigInteger`, Go `math/big`, Python and Clojure arbitrary-precision
   ints. Under this reading every language could honor any width — which is why
   the cap below is not a language limit but a format one.
2. **The shared cap is 15.** Every implementation MUST accept and compare
   exactly any `id_width` from 1 to 15. `10^15 − 1 = 999,999,999,999,999 < 2^53`,
   so every value of a ≤15-digit ID is exact even in the narrowest machinery any
   reader has (mawk, JS `number`). The counter space at width 15 is
   `10^15 − 1` items — about a billion times the width-6 space — so the cap
   never binds a real board.
3. **Widths above 15 are a format violation, everywhere.** This is rule 3
   applied: at 16+ the simplest reader cannot honor the grammar exactly, so
   every reader refuses loudly and uniformly. It also removes the T-0116
   divergence — both validators reject 16+, instead of Go rejecting 20+ while
   mawk accepts.
4. **The warning band stays "outside 3–6", within the honorable range.**
   Widths 1–2 and 7–15 are accepted with a warning (never a failure); 16+ is a
   violation. The recommend/warn/refuse ladder stays one clean rule.
5. **Counter arithmetic stays plain-native.** `next_id` increments and the I2
   comparison (`Num(id) >= Num(next_id)`) are exact at ≤15 digits with `int` in
   Go/Java, `number` in JS, and native ints in Python/Clojure — no bigint
   machinery anywhere. T-0118 (per-prefix counters) divides the same space per
   prefix and stays within it.

## 4. What lands as a result

- **T-0120 (spec change)**: bound `id_width` in §3.3.2 — "one to fifteen ASCII
  digits" — and restate rule 3: warn outside 3–6 within 1–15, refuse 16+; the
  `10^W − 1` cap note gains "for W ≤ 15". Reconcile §5.1 and the reference
  implementation notes that repeat the ID form.
- **T-0116 (validators)**: check.sh and the Go validator each enforce the 15
  cap (check.sh today accepts any digit count; Go today accepts up to int64).
  The pathological-width divergence disappears, and the fixture corpus can gain
  a width-16 broken fixture.
- **T-0117 (GUI)** and **T-0118 (per-prefix counters)** inherit the cap without
  change: the GUI only renders, and per-prefix counters fit under it.

## 5. Why not the alternatives

- **Cap at 18 (int64)**: Go and Java are exact, but mawk and naive JS silently
  round 16-digit IDs, and Go's own `Cap()` wraps at 19. The reference checker
  would be the weakest reader again — exactly the state this spike exists to end.
- **No cap, rely on arbitrary precision**: every language *can* do it, but the
  reference checker cannot, and "readers that cannot honor MUST refuse" would
  let validators disagree about which directories are valid. A format needs one
  grammar every reader honors, not per-reader capabilities.
- **Cap at 10 or 12**: safe but arbitrary; 15 is the principled boundary
  (the double-exactness limit, `2^53`) and costs nothing in headroom.

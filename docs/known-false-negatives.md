# Known False Negatives

Patterns that ProofAPI / LanguageTool currently does **not** detect.

| # | Input | Expected fix | Category | Notes |
|---|-------|-------------|----------|-------|
| 1 | `for to improve` | `to improve` | Grammar | "for to + infinitive" — non-standard in modern English, LT has no rule for this pattern |

---

## How to contribute a fix

If a false negative should be caught, options are:

1. **Custom rule** — add an XML rule to `languagetool/rules/en/` and rebuild the LT container
2. **Upstream issue** — report at https://github.com/languagetool-org/languagetool/issues
3. **Workaround** — enable a broader rule category in the request (`enabledCategories`)

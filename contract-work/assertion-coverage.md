# Assertion Coverage Report

**Generated:** 2026-04-28

**Source files:**
- `/Users/lakshmanpatel/.factory/missions/331f7496-52bd-4c4f-a4e8-7531e2d82bcb/validation-contract.md`
- `/Users/lakshmanpatel/.factory/missions/331f7496-52bd-4c4f-a4e8-7531e2d82bcb/features.json`

---

## Summary

| Category | Count |
|---|---|
| Total assertions in contract | 59 |
| Assertions claimed by features | 48 |
| **Orphans (defined, not claimed)** | **11** |
| **Duplicates (claimed by >1 feature)** | **0** |
| **Phantoms (claimed but not defined)** | **0** |

---

## 1. Orphan Assertions (defined in contract but not claimed by any feature)

These are real gaps: validation criteria with no corresponding feature to implement them.

### Code Quality & Architecture

| ID | Description |
|---|---|
| **VAL-CODE-002** | Engine tests use mock Storage — at least one test creates a mock/fake `Storage`. |

### Test Quality

| ID | Description |
|---|---|
| **VAL-TEST-013** | All 38 integration tests pass — full suite reports 38 PASS results. |
| **VAL-TEST-014** | All 38 tests pass with `-race` — zero data race warnings. |
| **VAL-TEST-015** | Tests use isolated DB per test — `t.TempDir()` for databases. |

### Cross-Area / Non-Regression (entire section unclaimed)

| ID | Description |
|---|---|
| **VAL-CROSS-001** | All 38 existing tests still pass with `-race` — no regressions. |
| **VAL-CROSS-002** | `go vet` passes without warnings — zero warnings. |
| **VAL-CROSS-003** | `go build` succeeds — all packages compile. |
| **VAL-CROSS-004** | Binary size ≤ 35 MB — stays within reasonable limit. |
| **VAL-CROSS-005** | Zero new external dependencies — no changes to `go.mod` require block. |
| **VAL-CROSS-006** | `CGO_ENABLED=0` builds succeed — compiles without CGO. |
| **VAL-CROSS-007** | Go SDK API unchanged — no breaking changes to public SDK. |

---

## 2. Duplicate Claims (claimed by more than one feature)

**None found.** Every claimed assertion ID appears in exactly one feature's `fulfills` array.

---

## 3. Phantom References (claimed but not defined in the contract)

**None found.** Every assertion ID referenced in a feature's `fulfills` array exists as a heading in the validation contract.

---

## Detailed Claim Mapping

### Features and their claimed assertions (11 features, 48 claims)

| Feature ID | Assertions Claimed |
|---|---|
| `extract-storage-interface` | VAL-CODE-001, VAL-CODE-003 |
| `extract-graph-interface` | VAL-CODE-004, VAL-CODE-005 |
| `add-context-propagation` | VAL-CODE-006, VAL-CODE-007 |
| `fix-code-quality-hotspots` | VAL-CODE-008, VAL-CODE-009, VAL-CODE-010, VAL-CODE-011, VAL-CODE-012, VAL-CODE-013, VAL-CODE-014, VAL-CODE-015, VAL-CODE-016, VAL-CODE-017, VAL-CODE-018, VAL-CODE-019, VAL-CODE-020 |
| `implement-graph-rank` | VAL-FEAT-007, VAL-FEAT-008, VAL-FEAT-009 |
| `implement-real-staleness` | VAL-FEAT-001, VAL-FEAT-002, VAL-FEAT-003, VAL-FEAT-010, VAL-FEAT-011 |
| `implement-local-onnx-embeddings` | VAL-FEAT-004, VAL-FEAT-005, VAL-FEAT-006 |
| `implement-paginated-vector-search` | VAL-FEAT-012, VAL-FEAT-013, VAL-FEAT-014 |
| `implement-memory-promotion` | VAL-FEAT-015, VAL-FEAT-016, VAL-FEAT-017 |
| `add-per-package-unit-tests` | VAL-TEST-001, VAL-TEST-002, VAL-TEST-003, VAL-TEST-004 |
| `add-table-driven-edge-case-tests` | VAL-TEST-005, VAL-TEST-006, VAL-TEST-007, VAL-TEST-008, VAL-TEST-009 |
| `add-race-condition-tests` | VAL-TEST-010, VAL-TEST-011, VAL-TEST-012 |

### Unclaimed assertions in each area

| Area | Total | Claimed | Unclaimed |
|---|---|---|---|
| VAL-CODE | 20 | 19 | 1 (VAL-CODE-002) |
| VAL-FEAT | 17 | 17 | 0 |
| VAL-TEST | 15 | 12 | 3 (VAL-TEST-013, 014, 015) |
| VAL-CROSS | 7 | 0 | 7 (all) |

---

## Recommended Follow-up

1. **Add a feature for non-regression assertions** — The entire `VAL-CROSS-*` section (7 assertions) has no owning feature. Consider `add-non-regression-checks`.
2. **Reassign or create a feature for VAL-CODE-002** — Mock storage testing is closely related to `extract-storage-interface` and could be added to that feature's `fulfills`.
3. **Add test-quality regression features** — VAL-TEST-013/014/015 could be owned by an `add-existing-test-regression` feature.

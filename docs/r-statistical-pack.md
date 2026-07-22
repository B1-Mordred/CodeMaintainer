# R statistical validation capability pack

`r-statistical-validation@1.0.0` is a checksummed, controller-registered profile for reproducible R package and statistical-application verification. It contributes fixed operation, parser, policy, context, documentation, and rehearsal identifiers; it cannot contribute executable code or select a command, image, mount, network, host path, environment variable, or tool argument.

## Verification profile

The operator can select a project-isolated, locked-offline, or disabled `renv` cache policy and enable the registered `R CMD check`, `testthat`, `lintr`, `roxygen2`, and `pkgdown` operations. Selecting an operation does not install its prerequisite. Catalog and preview responses report the required pinned R toolchain, and an unavailable prerequisite stays visible rather than being silently substituted.

The typed comparison profile controls:

- absolute and relative numeric tolerances from 0 through 1;
- exact, paired-ignore, or reject behavior for missing values;
- one repository-relative golden dataset reference, with traversal rejected;
- review-required or disabled golden-baseline updates;
- a deterministic non-negative 32-bit random seed;
- the trusted `C` or `en_US.UTF-8` locale and `UTC` time zone;
- bounded table, model, chart, and serialized-result comparison selections.

The browser groups these controls into safe typed fields and renders the selected golden reference, tolerances, seed, locale, time zone, update policy, and result classes as an inspection summary. It has no raw configuration or runner-command editor. The controller normalizes the same schema for Repo Doctor acceptance, assignment preview, assignment update, REST, and `maintainctl`.

## Golden evidence boundary

The manifest registers the `r-reference-results` rehearsal and declares table, model, chart, and reproducibility-metadata artifacts. The pack configuration can require review for a baseline update, but it cannot approve one. Durable artifact provenance, rendered result diffs, and evidence-bound golden approval are workflow policy records delivered by the golden/rehearsal layer; a failing rehearsal never updates its own reference.

For automation, create a partial or complete typed document and preview it before applying it. Omitted registered fields receive controller-owned defaults:

```json
{
  "tolerance": {"absolute": 0.000001, "relative": 0.000001},
  "golden": {
    "dataset_reference": "tests/golden",
    "update_policy": "review-required"
  },
  "random": {"seed": 42},
  "environment": {"locale": "C", "timezone": "UTC"},
  "comparison": {"tables": true, "models": true, "charts": true}
}
```

```text
maintainctl pack configure-preview --revision 1 --config r-profile.json --reason "inspect numeric comparison policy" PROJECT_ID r-statistical-validation
maintainctl pack configure --revision 1 --config r-profile.json --reason "approve numeric comparison policy" PROJECT_ID r-statistical-validation
```

Both commands reject an unknown field, unsafe reference, invalid enum, out-of-range tolerance or seed, stale revision, or non-object document.

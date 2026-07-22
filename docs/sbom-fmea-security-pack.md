# SBOM, FMEA, and security capability pack

`sbom-fmea-security@1.0.0` provides a checksummed declarative profile for selectable security evidence and release-risk correlation. It names only registered scanner, parser, policy, context, documentation, and rehearsal identities. Configuration never supplies scanner commands, database URLs, images, mounts, networks, paths, credentials, CodeQL arguments, or arbitrary policy input.

## Scanner and evidence profile

The project selects Syft SBOM generation and, independently, the registered Grype, Trivy, and supported/licensed CodeQL profiles. The controller never interprets one selection as permission to run the other scanners. The operator also selects an opaque controller-registered database profile, an explicit operator-refresh or offline-pinned update policy, and an immutable database snapshot reference. Dependency preparation owns database updates; normal verification does not gain egress from this configuration.

The typed browser editor manages:

- new-finding severity and normalized confidence thresholds;
- an optional reviewed suppression reference with a mandatory bounded reason and RFC3339 expiry;
- an operator-maintained risk-matrix profile and FMEA record set;
- optional FMEA correlation and project-scoped evidence-link set;
- operator-maintained CAPEC and ATT&CK reference mappings, which are displayed only as references and never as proof of exploitability;
- an immutable approved SBOM baseline reference;
- dependency, license, interface, privilege, and data-flow diff selection;
- observe, block-new-high, or block-new-critical release behavior.

The browser renders the selected scanners, database identity, release threshold, SBOM baseline, FMEA/risk/evidence references, mappings, and diff/release settings as an inspection summary. It has no raw JSON, scanner-command, or policy editor. Repo Doctor acceptance and later updates pass through the identical controller normalizer.

## Suppression and release boundary

A suppression is invalid unless reference, reason, and expiry are present together; reason or expiry without a reference is also rejected. Assignment configuration selects an existing reviewed record but does not create a vulnerability disposition. Similarly, the pack registers the `sbom-release-diff` rehearsal and its SBOM, diff, and risk-matrix artifacts, but the workflow policy layer owns durable findings, suppressions, approval evidence, and release decisions. Scanner output and repository content remain untrusted evidence.

Example partial configuration, with omitted fields filled from trusted defaults:

```json
{
  "scanner": {"syft": true, "grype": true, "trivy": false, "codeql": false},
  "database": {
    "profile": "offline-current",
    "update_policy": "offline-pinned",
    "snapshot_reference": "db-2026-07"
  },
  "threshold": {"severity": "high", "confidence": 0.8},
  "risk": {"matrix_profile": "default-fmea"},
  "fmea": {"record_set": "project-fmea", "enabled": true},
  "evidence": {"reference": "security-evidence"},
  "sbom": {"baseline_reference": "initial"},
  "release": {"gate": "block-new-high"}
}
```

```text
maintainctl pack configure-preview --revision 2 --config security-profile.json --reason "inspect release evidence policy" PROJECT_ID sbom-fmea-security
maintainctl pack configure --revision 2 --config security-profile.json --reason "approve release evidence policy" PROJECT_ID sbom-fmea-security
```

The controller rejects unknown scanner names, malformed identifiers or dates, incomplete suppressions, confidence outside 0 through 1, stale revisions, duplicate JSON keys, and oversized documents.

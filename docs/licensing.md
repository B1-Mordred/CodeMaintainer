# Licensing and software inventory

The appliance source in this repository is not granted a distribution license unless the repository owner adds one. Do not redistribute it by assumption.

Material runtime dependencies include Go and its modules (`go.mod`/`go.sum`), Node packages (`web/package-lock.json`), Docker base images pinned in Dockerfiles/Compose, llama.cpp at the revision recorded by the inference image, NousResearch Hermes Agent v2026.7.7.2 (MIT), OpenViking v0.3.21 (AGPL-3.0-only), Caddy 2.11.4, and Prometheus 3.12.0. OpenViking is unmodified, isolated behind an optional profile, carries source correspondence metadata, and its network use may trigger AGPL obligations; review those obligations before deployment or modification.

Run `scripts/sbom.sh` to emit `artifacts/sbom/` inventories from the pinned Go modules, npm lockfile, Compose image references, and built images when Syft is installed. Review upstream licenses and model licenses/source URIs before importing weights. The inventory is evidence, not legal advice.

Run `scripts/vulnerability-scan.sh` before promotion. It executes the pinned Go vulnerability analyzer and high-severity npm audit; when Docker Scout is installed it also fails on high or critical findings in every resolved Compose image. If Scout is unavailable, the script reports the required operator OCI-scanner follow-up instead of claiming image coverage.

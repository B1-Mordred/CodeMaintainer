# Models

Production models are local GGUF files imported through schema-v1 manifests in an immutable manifest directory. A manifest fixes ID, role, family, filename, SHA-256, bytes, source, license, quantization, context, threads, batch/ubatch, NUMA strategy, minimum RAM, and sampling. Import rejects path traversal, unknown roles, incompatible sizes, and checksum mismatches.

Start from `config/examples/models`. Use `maintainctl model list` and `maintainctl model benchmark <profile>`. Benchmark physical-core and SMT thread counts separately; on multi-node hosts compare NUMA local and interleave modes. Select the lowest stable latency profile that meets context and quality requirements, not simply all logical CPUs.

The supervisor loads at most one allow-listed model at once. Implementation and QC roles must remain distinct. Workers reach inference only through the inference-only network and cannot alter model paths or llama.cpp arguments. The mock profile is the required CI path and uses no weights.

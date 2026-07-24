# Models

Production models are local GGUF files imported through schema-v1 manifests in an immutable manifest directory. A manifest fixes ID, role, family, filename, SHA-256, bytes, source, license, quantization, context, threads, batch/ubatch, NUMA strategy, minimum RAM, and sampling. Import rejects path traversal, unknown roles, incompatible sizes, and checksum mismatches.

Start from `config/examples/models`. Use `maintainctl model list` and `maintainctl model benchmark <profile>`. Benchmark physical-core and SMT thread counts separately; on multi-node hosts compare NUMA local and interleave modes. Select the lowest stable latency profile that meets context and quality requirements, not simply all logical CPUs.

## Runtime benchmark lab

`maintainctl model benchmark <profile>` now retains an append-only runtime
benchmark record in the controller database. `maintainctl model benchmarks
[--profile <profile>]` lists the retained evidence. The Models page shows the
same latest benchmark and history.

Each retained run records:

- the exact manifest-derived runtime identity hash;
- model family, checksum when present, quantization, context, threads, batch,
  ubatch, NUMA, and sampling-derived identity;
- prompt/decode timing, elapsed duration, and memory evidence exposed by the
  supervisor;
- a deterministic smoke quality fixture, determinism status, cache eligibility,
  experimental feature gates, recommendation, reason, actor, and timestamp.

Candidate recommendations are evidence for operator review only. They do not
activate a profile, change a route, enable prompt-prefix caching, or switch on
speculative decoding, KV-cache quantization, experimental kernels, or other
runtime features. Any later activation workflow must bind to the retained
identity and quality evidence and remain reversible.

The supervisor loads at most one allow-listed model at once. Implementation and QC roles must remain distinct. Workers reach inference only through the inference-only network and cannot alter model paths or llama.cpp arguments. The mock profile is the required CI path and uses no weights.

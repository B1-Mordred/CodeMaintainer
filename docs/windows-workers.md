# Windows worker, simulator, and lab-automation security

The Runners and Windows page is the routine operator surface for disposable Windows verification. `maintainctl windows-worker` and `/api/v1/windows-workers` call the same controller service. The built-in `windows-simulator` profile is created automatically and is the normal CI path; it requires no Windows license, VM, credential, VPN, signing service, or physical instrument.

## Closed authority boundary

The controller accepts exactly ten operation classes: pinned .NET restore/build/test, PowerShell/Pester, Windows-service lifecycle, Inno Setup lifecycle, HAMILTON discovery, VPN workflow, release consistency, installer IQ evidence, equipment simulation, and signing request. Requests contain only exact repository, capability-pack, toolchain-inventory, source-artifact, release, service, HAMILTON-profile, and simulator-profile identities. There is no command, script, executable, image, mount, host path, network, environment, PowerShell argument, or arbitrary tool option field. Unknown JSON fields fail before dispatch.

The worker protocol is an authenticated, bounded JSON protocol with `/v1/probe` and `/v1/run`. Payloads are limited to 1 MiB, profiles and job types are registered on both sides, results are revalidated by the controller, and every retained result binds the canonical input SHA-256 and idempotency key. Checks and artifact metadata have count, size, state, duration, media-type, and checksum bounds. The controller records profile changes and completed runs transactionally with audit events; retrying the same key and exact input replays one durable result after restart.

The deterministic simulator implements the exact adapter interface and produces structured evidence for every operation class. It refuses a profile whose mode is `remote`. Simulator profiles use only `simulator://windows-worker`, have no credential, and cannot enable hardware, signing, or production-VPN gates.

## Remote disposable worker adapter

Remote mode uses the same protocol client and result validation as the simulator. Configure a worker endpoint using HTTPS; loopback HTTP is accepted only for explicit tests. The selected endpoint must exactly match one profile allow-list entry. Set a safe opaque credential reference such as `windows-lab-main`, never a path or token.

On the controller host, create an operator-owned token file named after that reference below the fixed secret root:

```text
/run/secrets/windows-workers/windows-lab-main.token
```

The deployment may change the fixed root with `MAINTAINER_WINDOWS_WORKER_SECRET_ROOT` and mount it read-only. Each token must contain at least 32 random bytes. The resolver accepts only a safe basename plus `.token`, rejects traversal, symlinks, non-regular files, short tokens, and files over 4 KiB, and never returns token material through the API, CLI, UI, logs, audit, or profile storage. The remote protocol client is instantiated only after profile validation and uses the profile timeout.

Windows-worker credential handling is included in `config/redaction-coverage.json`, which is checked in CI with the broader configuration, provider, observability, support-bundle, memory, authentication, forge, and policy redaction surfaces.

Deploy the protocol handler inside an operator-managed disposable Windows VM service. Register the same profile ID, fixed operation set, VM-template identity, and pinned toolchain inventory on that service. Its concrete executor must reset to a known VM snapshot between jobs and implement only the registered operation handlers. The CodeMaintainer controller-side real adapter is complete and testable with a loopback protocol server; the licensed Windows image and environment-specific executor remain operator-owned external infrastructure.

## Operator gates

Physical hardware, code signing, and production VPN are disabled by default. Raising a profile gate, changing a worker credential reference, or changing a signing-policy reference requires administrator reauthentication. A signing or VPN run also requires a recently reauthenticated administrator gate on that exact request. Equipment runs default to registered simulator profiles; a physical-hardware identity additionally requires the hardware gate. Agents cannot directly submit signing, VPN, or hardware jobs and never receive their credentials.

Use the browser for profile inventory, health, capacity, VM template, pinned toolchains, job allow-list, timeout, HAMILTON/simulator profiles, retention, signing policy, gates, probes, and structured run evidence. Headless parity is available with:

```text
maintainctl windows-worker profiles
maintainctl windows-worker get windows-simulator
maintainctl windows-worker probe windows-simulator
maintainctl windows-worker run windows-simulator run-request.json
maintainctl windows-worker runs windows-simulator
maintainctl windows-worker save windows-lab-main profile.json
```

## External validation checklist

The following checks require authorized infrastructure and are not claimed by CI:

1. Create a licensed disposable Windows VM from the exact recorded template and enable virtualization outside CodeMaintainer.
2. Install the pinned .NET, PowerShell, Pester, and Inno versions shown in the profile; expose only the authenticated worker service over the admitted endpoint.
3. Probe from the Runners and Windows page and compare returned toolchain and VM identities exactly.
4. Run the .NET and PowerShell fixture first, then service and installer lifecycle tests from a fresh snapshot; confirm uninstall/residue and IQ artifacts.
5. Test HAMILTON and instrument integrations with simulator profiles before enabling any physical-hardware gate.
6. Separately reauthenticate and validate production VPN or signing only with approved external policy and disposable inputs. Confirm agents and artifacts never receive credentials or signing material.
7. Rotate the worker token file, reauthenticate the profile reference change if needed, probe again, and confirm the retired token fails.

Failures remain explicit and do not fall back from a remote profile to the simulator. The simulator is a separately selected profile, so an unavailable remote VM cannot silently convert production evidence into simulated evidence.

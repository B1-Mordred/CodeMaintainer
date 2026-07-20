# Architecture

The controller is the only workflow-state authority. It stores configuration revisions, jobs, phases, findings, approvals, audit events, automation, memory metadata, and delivery replay state in SQLite. The static React dashboard and `maintainctl` call the same versioned OpenAPI surface.

`runnerd` owns a narrow server-side container policy. Requests select a job, project, phase kind, exact commit, and bounded input; they cannot select images, mounts, networks, capabilities, host paths, commands, or arbitrary environment. The Git bridge alone handles repository credentials and publication. The model supervisor alone maps versioned allow-listed profiles to one sequential llama-server child. Implementation and QC agents use distinct roles, prompts, contexts, and fresh model families. OpenViking and Hermes are optional replaceable integrations.

The default flow is sync, isolated worktree, dependency preparation, acceptance-criteria lock, implementation, targeted and full verification, independent QC, bounded repair, final verification, authenticated exact-SHA approval, branch publication, and idempotent draft PR creation. Every phase is durable and restart-resumable. Worktrees are disposable; artifacts and decisions are immutable evidence.

Trust boundaries and abuse controls are detailed in [security.md](security.md).

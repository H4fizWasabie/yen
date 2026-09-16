# Yen / Theoses2 feature-parity gap audit — v3

Date: 2026-09-16  
Scope: cross-platform portability and OS-facing runtime behavior  
Oracle: Theoses2 TypeScript pinned by `docs/UPSTREAM.md` at
`3a910426a0db91570392c20c281ae8dfd82e01a1`

This is a read-only audit. It records behavior gaps; it does not implement
them. The v2 implementation changes currently present in the worktree were
left untouched.

## Conclusion

Yen is currently Linux-first, not portable across the operating systems
supported by Theoses2.

Evidence from the current checkout:

- `GOOS=linux GOARCH=amd64 go build ./...` passes.
- `GOOS=darwin GOARCH=amd64 go build ./...` passes, but non-Linux TUI code is
  a stub.
- `GOOS=windows GOARCH=amd64 go build ./...` fails because Unix `syscall.Flock`
  and `LOCK_*` constants are used by the auth and conversation packages.

Theoses2 has explicit Windows, macOS, and Unix handling in its shell,
process, path, terminal, clipboard, and transport code. Yen currently has
Linux implementations plus non-Linux fallbacks in only some TUI files.

## Findings

### P0 — Windows cannot build Yen

`internal/auth/flock_unix.go` and `internal/conversation/flock_unix.go` call
`syscall.Flock` and Unix lock constants without a Windows implementation or a
usable platform abstraction. The Windows cross-build fails with undefined
`syscall.Flock`, `syscall.LOCK_SH`, `syscall.LOCK_EX`, and `syscall.LOCK_UN`.

Theoses2's durable stores do not make the application compile-time dependent
on these Unix-only APIs. Yen needs platform-specific locking or a portable
locking strategy before Windows is a supported target.

### P1 — macOS and Windows interactive TUI behavior is not implemented

Yen's non-Linux fallbacks are explicit stubs:

- `internal/tui/pty_other.go:7-9` returns “live terminal acceptance is only
  supported on linux”.
- `internal/tui/terminal_raw_other.go:7-9` disables raw input.
- `internal/tui/terminal_size_other.go:7-9` returns a 1 MiB by 1 MiB terminal
  size rather than querying the terminal.

Theoses2 provides platform-aware terminal behavior and has native macOS
terminal support (`packages/tui/native/darwin`), Windows-specific key and
shell handling, and terminal lifecycle cleanup in the interactive runtime.

Impact: a successful macOS build does not mean the interactive CLI is usable,
and Windows cannot reach this stage until the build failure is fixed.

### P1 — Shell selection and process-tree termination are Linux-centric

Yen defaults to the literal executable `bash` (`internal/tools/bash.go:68-80`)
and the PowerShell tool defaults to the literal executable `powershell`
(`internal/tools/bash.go:72-74`). It does not resolve Git Bash, `bash.exe`,
`pwsh.exe`, or a Unix `sh` fallback by platform. `exec.CommandContext` also
does not provide the oracle's explicit detached-child/process-tree cleanup.

Theoses2 resolves configured shells, Git Bash and PATH candidates on Windows,
falls back from `/bin/bash` to `sh` on Unix, and uses `taskkill /T` on Windows
versus process-group termination on Unix
(`packages/coding-agent/src/utils/shell.ts:20-21,24-57,67-135,193-240`).

Impact: shell tools can fail on valid Windows installations, and timed-out or
aborted commands may leave descendants running.

### P1 — RPC transport is Unix-socket-only

Yen exposes only `ServeUnix` and `DialUnix` using `net.Listen("unix", path)`
and `net.Dial("unix", path)` (`internal/rpc/transport.go:14-57,100+`). The
transport also assumes filesystem socket lifecycle and Unix permission modes.

Theoses2 has an explicit Unix transport boundary with platform-specific socket
path limits and Windows handling (`packages/server/src/transports/unix/listener.ts:16,379+`),
plus client/server transport tests. Yen has no named-pipe, TCP, or other
Windows-safe RPC transport and no platform-specific socket policy.

Impact: the RPC/dashboard/CLI composition cannot be treated as portable merely
because the Go packages cross-compile on macOS.

### P2 — Deployment packaging is Linux/systemd-specific

`deploy/install-side-by-side.sh` directly manages `systemctl` services. This is
appropriate for the current VPS deployment, but it is not an application
portability path for macOS or Windows. The deployment boundary should be kept
separate from portable binaries, with platform-specific service/install
instructions or installers documented independently.

### P2 — No cross-platform CI gate is visible

The repository has Linux-focused baseline evidence and no visible workflow or
Make target that continuously compiles/tests Windows and macOS. Cross-builds
should be added as a minimum gate; native TUI acceptance should remain
platform-specific and be reported separately from compile success.

## Recommended implementation order

1. Add portable file-locking implementations and make Windows compile.
2. Add cross-platform shell resolution and child-process cleanup.
3. Choose a Windows-safe RPC transport while preserving Unix sockets on Unix.
4. Implement real macOS/Windows terminal sizing, raw input, and PTY support,
   or explicitly make the CLI headless outside Linux until that work exists.
5. Add Windows/macOS cross-build CI and platform-specific acceptance jobs.
6. Keep the Linux/systemd deploy script as a separate operational adapter.

No code, release, deployment, restart, memory, channel, or session changes
were made for this audit.

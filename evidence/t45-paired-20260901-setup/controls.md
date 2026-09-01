# T-45 paired follow-up controls

All station setup and health commands below used the own worktrees only. The
shared `/Users/seth_wang/ai_workspace/OffiCraft` tree and production `:7755`
were not targets.

## Pair launch

The same launch exec started these two commands concurrently:

```text
bash /Users/seth_wang/ai_workspace/officraft-t45-pair-nohup/e2e_test/setup.sh
OC_E2E_PORT=8793 bash /Users/seth_wang/ai_workspace/officraft-t45-pair-tmux/e2e_test/setup.sh
```

Both setup commands returned `rc=0`. The raw setup stdout/stderr, listener
state, PID probes, and tmux probes are the sibling files in this directory.

## Nohup-alone control

After both pair runs were torn down with their own `teardown.sh`, the old
c8d2506f nohup checkout was run alone.

```text
setup_start
2026-09-01T10:51:57+0800
[setup] serve healthy AND identity-verified — git_sha=c8d2506f listener pid=40738 (launch pid=40737)
[setup] ✅ ready — base=http://127.0.0.1:8791  token→/Users/seth_wang/ai_workspace/officraft-t45-pair-nohup/e2e_test/.state/owner.tok
setup_rc=0
setup_end
2026-09-01T10:52:12+0800
```

The next independent exec returned:

```text
probe_start
2026-09-01T10:52:20+0800
api
curl: (7) Failed to connect to 127.0.0.1 port 8791 after 0 ms: Couldn't connect to server

http_code=000
rc=7
listener
rc=1
recorded pid
40738
ps
rc=1
```

The old serve log still ended with `ocserverd serving on
http://127.0.0.1:8791`; it contained no fatal shutdown message.

## Minimal background control

A separate minimal `nohup` heartbeat process was started in one exec at
`2026-09-01T10:52:51+0800` with PID `42022`. The next exec at
`10:52:58+0800` returned `ps_rc=1`, showed one heartbeat line, and showed
`<no signal log>`. This supports that ordinary background lifecycle is not a
reliable carrier in this runtime, but it does not identify the exact signal or
system component that removed the process.

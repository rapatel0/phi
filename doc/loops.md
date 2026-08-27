# Loops and monitors

Waiting used to cost a turn. A recurring check meant a shell `sleep` inside a
`bash` call, which holds the tool loop open for the whole wait. A long test run
meant watching it finish before anything else could happen.

Two tools replace that. A loop runs a prompt on a schedule. A monitor runs a
command in the background.

## Loops

`LoopCreate` takes a prompt and a schedule:

```text
LoopCreate  prompt="check whether the deploy finished"
            schedule="5m"
            maxFires=12
```

The schedule is an interval or a cron expression.

| Form | Example | Fires |
| --- | --- | --- |
| Interval | `30s`, `5m`, `2h` | That long after the last fire |
| Cron | `0 9 * * 1-5` | 9:00 on weekdays |

Use cron when the wall-clock moment matters. A daily check written as `24h`
drifts by the runtime of every fire.

`maxFires` stops the loop after that many fires. Set it for anything that polls
for a condition. Omit it only for a genuine watchdog, or the loop runs until
the session ends.

The minimum interval is 10 seconds. A loop that fires faster than a turn
completes starts the next one before the last finished.

### Managing loops

| Tool | Effect |
| --- | --- |
| `LoopList` | State, next fire, remaining fires, last error |
| `LoopUpdate` | Pause or resume |
| `LoopDelete` | Remove |

Pausing keeps the loop and its history. Resuming recomputes the next fire from
now, so a loop paused over a weekend does not fire once for every slot it
missed.

A completed iteration is not a reason to delete a loop. Recurring loops are
meant to persist. Pause instead when in doubt.

### When a fire cannot reach the agent

A loop fires by starting a turn. If a turn is already streaming, the shell
refuses and the loop records the reason:

```json
{"id": "loop-1", "state": "active", "lastError": "wake: a turn is already running"}
```

The loop keeps its schedule and tries again at the next slot. The reason is
kept because a loop that silently fails to wake anyone looks exactly like a
loop with nothing to report.

## Monitors

`MonitorCreate` starts a command and returns at once:

```text
MonitorCreate  command="go test ./..."
```

| Tool | Effect |
| --- | --- |
| `MonitorList` | State, exit code, runtime |
| `MonitorLogs` | The tail of the output |
| `MonitorStop` | Stop the command |

`MonitorLogs` returns the last 50 lines by default. The tail is kept rather
than the head, because a failure explains itself at the end. Output is capped
at 200 lines, so a build that prints a megabyte does not reach the model.

A command that was stopped reports `stopped`, not a failing exit code. It did
not fail on its own terms.

Every monitor stops when the session ends. A background build does not outlive
the session that started it.

### Permission

A background command is still a command. `MonitorCreate` passes the same
permission gate as `bash`, and a denial reports the reason:

```text
monitor: "rm -rf /" not allowed: matches a deny rule
```

Anything short of `Allow` is refused. A background command cannot show a
prompt, so a command that would need one cannot run unattended.

The gate arrives from the shell at startup. Until then `MonitorCreate` refuses,
which is the correct answer in a headless run: a supervisor without a gate
would be a way around a denied `bash` call.

## Seeing background work

The footer shows what is running:

```text
gemini · profile:work · 2 loops · 1 running
```

Paused loops are not counted. Background work nobody can see is background work
nobody stops.

## For extension authors

`internal/ext/loop` is the worked example of `ext.Background`. See
[ext-api.md](ext-api.md) for the interface and the rules the shell enforces.

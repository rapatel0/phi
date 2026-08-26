# Profiles

A profile is a named set of credentials. Each one has its own `auth.json`, so a
work login and a personal login exist side by side instead of overwriting each
other.

## Why

One person often holds several accounts for the same provider: a work
subscription and a personal one, or a client's key kept apart from their own.
With a single `auth.json`, moving between them means logging in again, which
discards the credential that was already there.

## Commands

```text
alpha profile list            list profiles and the providers each holds
alpha profile show            print the active profile name
alpha profile create <name>   create an empty profile
alpha profile use <name>      make <name> the default for later commands
alpha profile delete <name>   delete a profile and its credentials
```

`alpha profile list` shows which providers each profile is logged in to:

```text
* default          anthropic, gemini
  work             anthropic, codex
  personal         xai

active: default (from default)
```

## Selecting a profile

The active profile is chosen in this order:

1. `ALPHA_PROFILE` in the environment
2. the profile set by `alpha profile use`
3. `default`

The environment wins, so one shell or one project can differ from the rest:

```sh
export ALPHA_PROFILE=work     # this shell only
ALPHA_PROFILE=work alpha      # this command only
```

Put the export in a `direnv` `.envrc` to switch profile per project.

Use `alpha profile use` for the machine-wide default. If `ALPHA_PROFILE` is
also set, the command says so rather than appearing to do nothing.

## Where the files live

| Profile | Credentials |
| --- | --- |
| `default` | `~/.alpha/auth.json` |
| `<name>` | `~/.alpha/profiles/<name>/auth.json` |

The default profile keeps the original path, so an existing install needs no
migration and nothing moves.

Only credentials are per profile. Sessions, skills, hooks, and configuration
stay shared, because they describe the machine rather than the account.

## Logging in to a profile

`alpha login` writes to the active profile:

```sh
alpha profile create work
ALPHA_PROFILE=work alpha login anthropic
```

Every path to a credential goes through the active profile, so no command needs
a profile flag.

## Deleting

`alpha profile delete` removes the directory and the tokens in it. Deleting the
profile currently pointed at also clears the pointer, because a pointer to a
missing directory reads as being logged out of everything.

The default profile cannot be deleted: it is the global directory, which also
holds sessions, skills, and hooks.

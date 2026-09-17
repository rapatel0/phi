# Upstream agent sync

`.github/workflows/upstream-agent-sync.yml` checks `pulseaiclub/phi` every six
hours and on manual dispatch.

When upstream `main` moves, the workflow performs these steps:

1. Fetch upstream `main`.
2. Start `alpha run` with a CI-only agent prompt.
3. Run tests and format checks.
4. Commit the agent changes on an automation branch.
5. Open a pull request against `main`.

The workflow never pushes directly to `main`. Review and merge each pull
request. The agent prompt also forbids changes to the sync workflow itself.

## Required repository secrets

Add these repository secrets before the first scheduled run:

- `ALPHA_API_KEY`: API key for the update agent.
- `ALPHA_MODEL`: model name for the update agent.
- `ALPHA_BASE_URL`: optional OpenAI-compatible API base URL.

The workflow passes these values only to the `alpha run` process. It does not
print them.

## Manual run

Open **Actions → Upstream agent sync → Run workflow** to check upstream before
the next scheduled run.

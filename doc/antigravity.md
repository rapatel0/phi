# Antigravity

Antigravity is Google's agent IDE. It reaches Gemini 3 and Claude 4.6 models
through an internal Cloud Code endpoint rather than the public Gemini API.

```sh
alpha login antigravity
```

The browser opens a Google consent screen. Approve it, and the login finishes
by itself: the consent redirects to a local server on the port registered with
the OAuth client.

If the browser runs on another machine, that redirect cannot reach this
process. Paste the URL you land on instead. The page will not load, so copy the
address from the browser bar.

The same applies when the port is already held by another alpha. Binding it is
not required, because paste always works.

## Models

The catalog registers these under the `antigravity` provider:

| Model | Context |
| --- | --- |
| `antigravity-gemini-3.1-pro` | 1M |
| `antigravity-gemini-3.7-flash` | 1M |
| `antigravity-gemini-3.6-flash` | 1M |
| `antigravity-claude-sonnet-4-6-thinking` | 200K |
| `antigravity-claude-opus-4-6-thinking` | 200K |
| `antigravity-gpt-oss-120b-medium` | 128K |

Names keep the `antigravity-` prefix so they cannot collide with the public
Gemini models in the same palette. The transport strips it before sending.

## How it works

The request body is the ordinary Gemini `generateContent` body, so the
transport reuses the Gemini client's request building and response parsing.
Three things differ:

- The URL is `/v1internal:streamGenerateContent`, not a model path.
- The body is wrapped in an envelope carrying a project id and request type.
- The `User-Agent` identifies the Antigravity CLI. The endpoint refuses a
  generic agent.

The daily endpoint is tried first, matching the IDE, and production is used
when it refuses.

## This can stop working

The endpoint is undocumented and versioned `v1internal`. Google can withdraw
its client credentials at any time.

That failure arrives as a plain 401, 403, or 404, which is indistinguishable
from a bad login. Alpha reports it as a distinct condition instead:

```text
antigravity is no longer accepted by Google (HTTP 401): ...
```

The token endpoint reports the same for `invalid_client` and
`unauthorized_client`. When you see this message, logging in again will not
help: the provider is gone rather than the credential being wrong.

An ordinary server error keeps its own message, so a transient failure is not
mistaken for a withdrawal.

## Configure OAuth credentials

Set these variables before you run `alpha login antigravity`:

```sh
export ALPHA_ANTIGRAVITY_CLIENT_ID='your-client-id'
export ALPHA_ANTIGRAVITY_CLIENT_SECRET='your-client-secret'
```

Alpha does not store OAuth client credentials in its source tree.

## Limits

Multi-account rotation and quota gating are not ported. Use
[profiles](profiles.md) to keep more than one Google account.

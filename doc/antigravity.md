# Antigravity

Antigravity is Google's agent IDE. It reaches Gemini 3 and Claude 4.6 models
through an internal Cloud Code endpoint rather than the public Gemini API.

```sh
alpha login antigravity
```

The browser opens a Google consent screen. Approve it, then paste the URL you
land on. The page does not load; copy the address from the browser bar.

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

The endpoint is undocumented, versioned `v1internal`, and reached with the
credentials embedded in the shipped Antigravity application. Google can
withdraw either at any time.

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

## Limits

The embedded client id and secret are stored base64-encoded in
`internal/auth/antigravity.go`, matching how the Anthropic client id is kept.
They identify the application, not the user, and are not secrets.

Multi-account rotation and quota gating are not ported. Use
[profiles](profiles.md) to keep more than one Google account.

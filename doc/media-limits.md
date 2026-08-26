# Provider image limits

The budgets in `internal/ext/mediaguard` and `internal/media` come from these
published limits. Record the source when you change a number. A budget without
a source is a guess, and a guess that runs high produces an opaque rejection
from the provider.

Fetched from the official documentation. Re-check before changing a value.

## The base64 rule

Every provider states its limit on the **base64-encoded** payload. Base64 is
4/3 the size of the raw bytes. A raw budget compared against an encoded limit
overshoots by a third.

This caused two real defects:

- `media.maxOutBytes` was 4 MiB raw, which is 5.6 MB encoded, over the 5 MB
  floor that applies on Bedrock and Vertex.
- `mediaguard.DefaultMaxBytes` was 12 MiB raw, which is 16.8 MB encoded,
  against a 20 MB request cap that also has to hold the prompt.

`Budget.EncodedBytes` reports the wire size. Compare that against a documented
limit, never the raw count.

## Anthropic

Source: <https://docs.claude.com/en/docs/build-with-claude/vision>

| Limit | Value |
| --- | --- |
| Maximum dimensions | 8000x8000 px |
| Per image | 10 MB base64 direct, 5 MB on Bedrock and Vertex |
| Per request | 32 MB on standard endpoints |
| Images per request | 100 at 200k context, 600 otherwise |
| Server-side downscale | Yes: 1568 px long edge, 2576 px on Claude 4.7 and later |

Above 20 images in one request, a stricter per-image dimension limit applies to
every image in that request, and the documentation recommends keeping each
image under 2000 px. `BudgetFor("anthropic")` caps at 20 images to stay below
that threshold.

Anthropic resizes oversized images itself, so sending more pixels than the tier
accepts only costs upload time. The exception is a `tool_result` image, which
is rejected rather than downscaled.

## OpenAI

Source: <https://platform.openai.com/docs/guides/images-vision>

| Limit | Value |
| --- | --- |
| Per request | 512 MB total payload |
| Images per request | 1500 |
| File types | PNG, JPEG, WebP, non-animated GIF |
| Tiling | Fit to 2048 px box, shortest side to 768 px, count 512 px tiles |

The documented limits are far above anything a terminal session produces, so
the budget here bounds context growth rather than satisfying the API.

## Gemini

Source: <https://ai.google.dev/gemini-api/docs/image-understanding>

| Limit | Value |
| --- | --- |
| Inline request total | 20 MB for prompt, system instructions, and image bytes together |
| Files per request | 3600 |
| Tiling | 258 tokens under 384 px, otherwise 768x768 tiles at 258 tokens each |

The 20 MB covers the whole request, not just images, so the image budget takes
about half and leaves the rest for the turn. Larger payloads need the File API,
which alpha does not use.

## xAI

Source: <https://docs.x.ai/docs/guides/image-understanding>

| Limit | Value |
| --- | --- |
| Per image | 20 MiB |
| Image count | No documented limit |
| File types | JPEG and PNG only |

xAI is reached over the OpenAI-compatible path, so it is the backend most
likely to be mislabeled. `client.Provider` asks `auth.ProviderFor` rather than
assuming OpenAI, because OpenAI's budget is over three times xAI's documented
per-image limit.

xAI does not list GIF or WebP. `media.Normalize` can emit either when the input
is already small enough to pass through untouched. This is a known gap: the
budget is enforced per provider, but the format is not.

## Why the client still resizes

Anthropic and OpenAI both downscale server-side, so a client-side resize is not
required for correctness on those two. It still earns its place:

- Upload time and request size scale with the bytes sent, not the bytes kept.
- Gemini's 20 MB cap applies to what is sent, before any server-side handling.
- A `tool_result` image is rejected rather than resized by Anthropic.

`media.maxDim` stays at 2048 px: above every tier that resizes, so a provider
that does not resize still receives a usable image.

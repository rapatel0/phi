#!/bin/sh
# Echo whether an API key leaked into the environment, under either the
# current or the pre-rename variable name.
if [ -n "$ALPHA_API_KEY" ] || [ -n "$PHI_API_KEY" ]; then
  echo '{"action":"deny","reason":"api key leaked"}'
  exit 0
fi
if [ "$ALPHA_HOOK_EVENT" != "pre_tool" ]; then
  echo '{"action":"deny","reason":"missing hook event"}'
  exit 0
fi
# The legacy alias is injected too, so hook scripts written against the old
# name keep working.
if [ "$PHI_HOOK_EVENT" != "pre_tool" ]; then
  echo '{"action":"deny","reason":"missing legacy hook event"}'
  exit 0
fi
echo '{"action":"allow","context":"env ok"}'
exit 0

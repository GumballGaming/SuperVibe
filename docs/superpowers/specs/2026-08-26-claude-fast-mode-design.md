# Claude Fast Mode

## Goal

Expose the existing Fast composer control for every Claude model. Claude Code receives the existing `fastMode` request flag and applies it through its settings payload; this change only updates the UI capability policy.

## Behavior

- Claude sessions always show the Fast toggle, whether a model is selected or the default model is used.
- Codex sessions retain the current model metadata-based gating.
- Fast remains a per-composer-session setting and resets when switching sessions.
- Sending continues to pass `fastMode` through `SendMessageConfigured`.

## Implementation

Update the composer capability predicate so the Claude provider is considered fast-mode capable before falling back to the existing model metadata check. Keep the current button styling, accessible pressed state, tooltip, and request plumbing unchanged.

## Verification

Run the frontend typecheck, frontend test suite, and production build. Confirm the diff is limited to the composer policy and this design record, without staging unrelated worktree changes.

# Batch Terminal Setup

## Goal

Let the terminal setup flow create multiple terminals in one action. The user selects a quantity from 1 through 6, selects a branch and terminal type (Blank Terminal, Codex, or Claude), and the app creates exactly that many terminals before replacing the setup UI with the working terminal deck.

## Interaction

- The setup view presents quantity buttons 1–6.
- The selected branch applies to every terminal created in the batch.
- Choosing Blank Terminal, Codex, or Claude creates the selected quantity.
- Each terminal receives a distinct fixed slot, beginning with the lowest available slot.
- If fewer slots are available than requested, only the available slots are created and the user receives a concise informational message; no existing terminal is replaced.
- After creation, the setup bar is gone and the terminal deck/tab strip is shown. The existing deck remains available for switching, adding, and closing terminals.
- If the user cancels setup, the temporary setup terminal is removed as it is today.

## Architecture

The store owns batch creation so slot allocation and selection remain consistent. A new action creates terminals for a worktree with a requested count, branch, and kind, updates the selected workspace terminal to the first created terminal, and keeps the terminal tab state in the existing `terminalSessions` map. The setup component owns only the quantity selection and delegates creation to the store-backed callback.

The existing terminal mounting rule remains unchanged: once a configured terminal exists, its `TerminalView` stays mounted for the lifetime of the session. Codex and Claude use their current initial commands; blank terminals have no initial command.

## Error handling and compatibility

- Quantity is clamped to 1–6 at the UI/store boundary.
- Duplicate slot allocation is prevented by the store.
- Existing single-slot summon behavior remains available from empty terminal tabs after setup.
- Backend terminal startup failures continue to be handled by `TerminalView` and do not roll back sibling terminals in the batch.

## Verification

- Add store tests for creating one and multiple terminals, applying the selected kind, allocating the lowest available slots, and respecting the six-slot limit.
- Run the frontend test suite and TypeScript/build checks.
- Run the app and manually verify quantity selection, all three terminal types, setup-bar dismissal, and switching between created terminals.

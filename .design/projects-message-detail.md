# Projects message detail

The conversation's display preference has two values, **Detailed / Concise**
(**详细 / 简洁**). Detailed is the default and uses the existing rendering,
including its normal folded tools/reports. Tool expansion is still a separate
inspection action, not a third detail level.

- Project worker threads and folder threads: Concise hides tool-call rows,
  results and Agent tool cards. User/assistant messages, thoughts, errors,
  checklists and working status remain. Approval/question cards stay actionable;
  settled decisions stay as request rows when their normal tool row is hidden.
  Nested subagent steps use the same policy and are read through the existing
  lazy reader so a hidden Agent card cannot hide a request inside it.
- Project main thread (the manager/coordinator conversation): Concise shows
  only the manager's own assistant speech. User/peer messages, thread lifecycle
  cards, reports, suggestions and error entries are hidden. The composer,
  Overview and waiting signals are unchanged; users can return to Detailed or
  open a worker to act. This main-thread renderer does not have approval/ask
  response controls; worker controls remain in the shared transcript.

Both use `MessageDetailControl` next to the composer. A tooltip explains the
surface-specific meaning and that this is display only (UX P3/P8/P9). The two
surfaces are remembered separately, avoiding surprising cross-surface changes
(P12), with the existing guarded browser storage:

- `tingly-project.conversation.messageDetail`
- `tingly-project.thread.messageDetail`

Values are `detailed` / `concise`. Missing or invalid settings default to
Detailed. With no new conversation setting, legacy
`tingly-project.conversation.threadReports = 0` maps to Concise; all other old
values map to Detailed. The new setting takes precedence. `thread.expandTools`
is preserved and is not interpreted as visibility. Mounted panes and browser
tabs synchronize the corresponding preference; navigation/reload reads it back.

Filtering happens after event conversion and call/result/request pairing, not
in the event cache, transport, or execution path. Block indexes remain stable
for trajectory anchors. Explicit “show in chat” for a hidden call switches to
Detailed before revealing it. Trajectory remains a full inspection view, with
its existing independent inspection filters; the detail picker controls the
conversation, not the trajectory. Request draft/submission storage is unchanged,
so switching detail levels cannot repeat an accepted decision.

No API/schema or backend setting is needed for this per-browser convenience.

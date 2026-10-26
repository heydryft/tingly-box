import type { ThreadEvent } from '../../client';
import type { MessageDetail } from '../../transcript/messageDetail';

/** Concise hides only cross-thread conversational text. Human input, the
 * manager's own replies, and every structured card/control remain visible. */
export function showManagerMessage(message: ThreadEvent, detail: MessageDetail): boolean {
    if (detail === 'detailed') return true;
    const text = message.kind === 'user' || message.kind === 'assistant';
    return !(text && message.source === 'thread');
}

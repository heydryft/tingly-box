import type { ThreadEvent } from '../../client';
import type { MessageDetail } from '../../transcript/messageDetail';

/** Concise reduces conversational text, never structured cards or controls.
 * Unknown event kinds remain eligible for their renderer. */
export function showManagerMessage(message: ThreadEvent, detail: MessageDetail): boolean {
    if (detail === 'detailed') return true;
    if (message.kind === 'user') return false;
    if (message.kind === 'assistant') return message.source !== 'thread' && message.source !== 'system';
    return true;
}

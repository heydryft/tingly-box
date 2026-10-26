import type { ThreadEvent } from '../../client';
import type { MessageDetail } from '../../transcript/messageDetail';

/** The concise manager view contains only the manager's own speech, not peer
 * messages, thread lifecycle cards, reports or thread suggestions. */
export function showManagerMessage(message: ThreadEvent, detail: MessageDetail): boolean {
    return detail === 'detailed' || (message.kind === 'assistant' && message.source !== 'thread' && message.source !== 'system');
}

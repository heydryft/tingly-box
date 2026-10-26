import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import type { Thread, ThreadEvent } from '../../client';
import { ConversationPane } from './ConversationPane';
import { readLocal, writeLocal } from '../../utils/storage';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('../Markdown', () => ({ Markdown: ({ text }: { text: string }) => <div>{text}</div> }));
vi.mock('../ThreadComposer', () => ({ ThreadComposer: ({ onSend }: { onSend: (text: string, files: []) => Promise<boolean> }) => <button onClick={() => void onSend('New message', [])}>Send message</button> }));

beforeEach(() => localStorage.clear());
afterEach(cleanup);
const event = (id: string, kind: string, content: string, extra: Partial<ThreadEvent> = {}): ThreadEvent => ({
    id, kind, content, thread_id: 'main', created_at: '', updated_at: '', ...extra,
});
const messages = [
    event('u', 'user', 'User request'),
    event('a', 'assistant', 'Manager reply'),
    event('peer', 'assistant', 'Peer message', { source: 'thread' }),
    event('start', 'thread_started', '', { payload: JSON.stringify({ thread_ids: ['worker'] }) }),
    event('report', 'thread_update', 'Thread report', { payload: JSON.stringify({ thread_id: 'worker', title: 'Worker report', kind: 'finished', status: 'ready' }) }),
    event('suggest', 'suggestions', '', { payload: JSON.stringify({ suggestions: [{ title: 'Proposed task', task: 'Do it' }] }) }),
    event('err', 'error', 'Thread failed'),
];
const worker = { id: 'worker', title: 'Worker title', status: 'running', branch: '', commits_ahead: 0, files_changed: 0 } as Thread;
const props = () => ({
    projectId: 'project', threadId: 'main', messages, threads: new Map([['worker', worker]]), busy: false, disabled: false,
    onSend: vi.fn().mockResolvedValue(true), onOpenThread: vi.fn(), onStartSuggestions: vi.fn().mockResolvedValue(undefined),
});
const show = (p = props()) => render(<MemoryRouter><ConversationPane {...p} /></MemoryRouter>);

describe('manager message detail', () => {
    it('defaults to the complete view and concise shows only manager speech', async () => {
        const p = props();
        const original = JSON.stringify(p.messages);
        show(p);
        expect(screen.getByText('User request')).toBeInTheDocument();
        expect(screen.getByText('Manager reply')).toBeInTheDocument();
        expect(screen.getByText('Worker title')).toBeInTheDocument();
        expect(screen.getByText('Worker report')).toBeInTheDocument();
        expect(screen.getByText('Proposed task')).toBeInTheDocument();
        expect(screen.getByText('Thread failed')).toBeInTheDocument();
        fireEvent.click(screen.getByRole('button', { name: 'messageDetail.concise' }));
        expect(screen.getByText('Manager reply')).toBeInTheDocument();
        for (const text of ['User request', 'Peer message', 'Worker title', 'Worker report', 'Proposed task', 'Thread failed']) expect(screen.queryByText(text)).toBeNull();
        expect(readLocal('conversation.messageDetail')).toBe('concise');
        expect(JSON.stringify(p.messages)).toBe(original);
        expect(p.onStartSuggestions).not.toHaveBeenCalled();
        expect(p.onOpenThread).not.toHaveBeenCalled();
        await act(async () => fireEvent.click(screen.getByText('Send message')));
        expect(p.onSend).toHaveBeenCalledWith('New message', []);
        fireEvent.click(screen.getByRole('button', { name: 'messageDetail.detailed' }));
        expect(screen.getByText('Worker report')).toBeInTheDocument();
    });

    it('restores concise after remount and migrates old hidden-report settings', () => {
        writeLocal('conversation.threadReports', '0');
        const first = show();
        expect(screen.queryByText('Worker report')).toBeNull();
        expect(screen.getByRole('button', { name: 'messageDetail.concise' })).toHaveAttribute('aria-pressed', 'true');
        fireEvent.click(screen.getByRole('button', { name: 'messageDetail.detailed' }));
        first.unmount();
        const second = show();
        expect(screen.getByText('Worker report')).toBeInTheDocument();
        fireEvent.click(screen.getByRole('button', { name: 'messageDetail.concise' }));
        second.unmount();
        show();
        expect(screen.queryByText('Worker title')).toBeNull();
        expect(screen.getByText('Manager reply')).toBeInTheDocument();
    });
});

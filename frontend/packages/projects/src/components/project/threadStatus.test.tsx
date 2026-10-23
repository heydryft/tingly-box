import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { stateTone, ThreadState } from './threadStatus';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
afterEach(cleanup);

describe('ThreadState', () => {
    it('colours only what needs the user and what failed', () => {
        expect(stateTone('waiting')).toBe('attention');
        expect(stateTone('failed')).toBe('failed');
        for (const s of ['working', 'ready', 'idle', 'resolved']) expect(stateTone(s)).toBe('neutral');
    });

    it('shows the word beside the mark, or only the mark when compact', () => {
        const { rerender } = render(<ThreadState status="waiting" />);
        expect(screen.getByText('thread.status.waiting')).toBeTruthy();
        rerender(<ThreadState status="waiting" compact />);
        expect(screen.queryByText('thread.status.waiting')).toBeNull();
        expect(document.querySelector('[data-status="waiting"]')?.getAttribute('title')).toBe('thread.status.waiting');
    });
});

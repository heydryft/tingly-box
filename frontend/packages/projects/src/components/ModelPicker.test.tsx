import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Composer } from './Composer';
import { ModelPicker } from './ModelPicker';
import { ConversationPane } from './project/ConversationPane';
import { invalidateModels } from '../hooks/useModels';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

const { fetchModels } = vi.hoisted(() => ({ fetchModels: vi.fn() }));
vi.mock('../client', async (actual) => ({ ...await actual<typeof import('../client')>(), api: { GET: fetchModels } }));

beforeEach(() => {
    invalidateModels();
    fetchModels.mockReset();
    fetchModels.mockImplementation(async (path: string) => {
        const id = path.endsWith('/projects/models') ? 'host-model' : 'chat-model';
        return { data: { models: [{ id, display_name: id }] }, response: new Response() };
    });
});
afterEach(cleanup);

it('uses the host model list in project composers and worker pickers', async () => {
    const choose = vi.fn();
    const view = render(<ConversationPane projectId="p1" threadId="main" messages={[]} threads={new Map()} busy={false}
        disabled={false} model="" onModelChange={choose} onSend={vi.fn()} onOpenThread={vi.fn()} onStartSuggestions={vi.fn()} />);
    await waitFor(() => expect(fetchModels).toHaveBeenCalledOnce());
    fireEvent.click(screen.getByRole('button', { name: /model\.auto/ }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'host-model' }));
    expect(choose).toHaveBeenCalledWith('host-model');
    expect(fetchModels).toHaveBeenCalledWith('/api/v1/projects/models');
    view.unmount();

    render(<ModelPicker source="projects" value="" defaultLabel="Project default · host-model" onChange={choose} />);
    fireEvent.click(screen.getByRole('button', { name: 'Project default · host-model' }));
    expect(await screen.findByRole('menuitem', { name: 'Project default · host-model' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'host-model' })).toBeInTheDocument();
    expect(fetchModels).toHaveBeenCalledOnce();
});

it('can restore the inherited model without confusing an explicit matching model', async () => {
    const choose = vi.fn();
    render(<ModelPicker source="projects" value="host-model" defaultLabel="Project default · host-model" onChange={choose} />);
    fireEvent.click(screen.getByRole('button', { name: 'host-model' }));
    const reset = await screen.findByRole('menuitem', { name: 'Project default · host-model' });
    expect(reset).not.toHaveClass('Mui-selected');
    expect(screen.getByRole('menuitem', { name: 'host-model' })).toHaveClass('Mui-selected');
    fireEvent.click(reset);
    expect(choose).toHaveBeenCalledWith('');
});

it('keeps standalone Chats on their connection and allows custom model IDs', async () => {
    const choose = vi.fn();
    render(<Composer model="" onModelChange={choose} onSend={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'model.auto' }));
    expect(await screen.findByRole('menuitem', { name: 'chat-model' })).toBeInTheDocument();
    expect(fetchModels).toHaveBeenCalledWith('/api/v1/models');
    expect(screen.queryByRole('menuitem', { name: 'model.auto' })).not.toBeInTheDocument();
    const input = screen.getByPlaceholderText('model.customPlaceholder');
    fireEvent.change(input, { target: { value: 'custom-model' } });
    fireEvent.submit(input.closest('form')!);
    expect(choose).toHaveBeenCalledWith('custom-model');
});

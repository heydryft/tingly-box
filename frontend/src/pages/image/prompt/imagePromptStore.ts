import { USE_MOCK, createWorkspaceCollection } from '../imageWorkspaceDb';

// One Image to Prompt request and what came back. The picture is kept as the
// data URL that was sent, so a run can be re-run and handed on after a reload.
export interface ImagePromptRun {
    id: string;
    createdAt: number;
    image: string;
    imageName: string;
    // The rule name sent as `model` on /tingly/imageprompt.
    model: string;
    instructions: string;
    status: 'pending' | 'completed' | 'failed';
    // The prompt as the model wrote it, then as the user edited it.
    prompt: string;
    error?: string;
}

// Browser-local like the rest of the image workspace (imageWorkspaceDb.ts).
const runStore = createWorkspaceCollection<ImagePromptRun>(
    'imagePrompts',
    USE_MOCK ? () => import('./mockImagePrompts').then((module) => module.mockImagePromptRuns) : null,
);

export const useImagePromptRuns = runStore.useItems;
export const useImagePromptRunsReady = runStore.useReady;

// Newest first: the list reads top-down, and the run just started is the one
// being waited on.
export const addImagePromptRun = (run: ImagePromptRun) => {
    runStore.set([run, ...runStore.get().filter((item) => item.id !== run.id)]);
};

export const updateImagePromptRun = (id: string, fields: Partial<ImagePromptRun>) => {
    runStore.set(runStore.get().map((run) => (run.id === id ? { ...run, ...fields } : run)));
};

export const removeImagePromptRun = (id: string) => {
    runStore.set(runStore.get().filter((run) => run.id !== id));
};

export const clearImagePromptRuns = () => runStore.set([]);

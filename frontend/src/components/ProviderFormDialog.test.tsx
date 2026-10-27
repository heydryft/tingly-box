import {useState} from 'react';
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import {afterEach, beforeAll, describe, expect, it, vi} from 'vitest';
import i18n from '@/i18n';
import ProviderFormDialog, {type EnhancedProviderFormData} from './ProviderFormDialog';
vi.mock('@/services/serviceProviders', () => ({useProviderCatalogs: () => []}));
vi.mock('@/services/api', () => ({api: {getConfig: vi.fn().mockResolvedValue({data: {}})}}));
vi.mock('./ProviderExportButton', () => ({default: () => null}));
beforeAll(async () => {await i18n.changeLanguage('en');});
afterEach(cleanup);
function renderLocal(optionalEditableToken = false, noKeyRequired = false, selectedProviderId?: string) {
    const onSubmit = vi.fn();
    function Harness() {
        const [data, setData] = useState<EnhancedProviderFormData>({name: 'Local', apiStyle: 'openai', apiBase: 'http://127.0.0.1:11434/v1', token: '', noKeyRequired, selectedProviderId});
        return <ProviderFormDialog open onClose={() => {}} mode="add" data={data} onSubmit={onSubmit}
            optionalEditableToken={optionalEditableToken} onChange={(field, value) => setData(previous => ({...previous, [field]: value}))}/>;
    }
    render(<Harness/>);
    return onSubmit;
}
describe('self-hosted API keys', () => {
    it.each(['', '   '])('accepts an empty or whitespace key as no authentication: %j', async token => {
        const submit = renderLocal();
        expect(screen.getByLabelText(/API Key/)).not.toBeRequired();
        expect(screen.getByLabelText('HTTP/SOCKS Proxy URL (Optional)')).toBeVisible();
        fireEvent.change(screen.getByLabelText(/API Key/), {target: {value: token}});
        fireEvent.click(screen.getByRole('button', {name: 'Connect'}));
        await waitFor(() => expect(submit).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({token: '', noKeyRequired: true})));
    });
    it('still requires a key for an explicitly selected hosted preset unless no authentication is selected', () => {
        const submit = renderLocal(false, false, 'openai');
        expect(screen.getByLabelText(/API Key/)).toBeRequired();
        fireEvent.click(screen.getByRole('button', {name: 'Connect'}));
        expect(submit).not.toHaveBeenCalled();
        expect(screen.getByText('Enter an API key or select “No API key required”.')).toBeInTheDocument();
    });
    it('enables authentication when a key is entered for an optional-key local preset', async () => {
        const submit = renderLocal(true, true);
        fireEvent.change(screen.getByLabelText(/API Key/), {target: {value: ' local-secret '}});
        fireEvent.click(screen.getByRole('button', {name: 'Connect'}));
        await waitFor(() => expect(submit).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({token: 'local-secret', noKeyRequired: false})));
    });
});

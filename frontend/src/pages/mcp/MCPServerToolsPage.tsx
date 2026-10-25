import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { IconButton, Stack, Typography } from '@mui/material';
import { Refresh } from '@/components/icons';
import { PageLayout } from '@/components/PageLayout';
import MCPSourceDrawer from './MCPSourceDrawer';
import MCPToolsPanel from './MCPToolsPanel';
import MCPWorkspaceAlerts from './MCPWorkspaceAlerts';
import { isServerToolSource } from './toolPresentation';
import { useMCPWorkspace } from './useMCPWorkspace';

// Tools Tingly Box runs itself, on the server. They need no MCP connection and
// work as soon as they are on, so they are kept apart from connected MCPs: no
// profile, just a switch and the tool's own settings.
export default function MCPServerToolsPage() {
    const { t } = useTranslation();
    const label = (key: string, fallback: string) => t(`mcp.workspace.${key}`, { defaultValue: fallback });
    const [search, setSearch] = useSearchParams();
    const ws = useMCPWorkspace();
    const { config, routing, loading, checking, busy } = ws;
    const sources = (config.sources || []).filter(isServerToolSource);
    const selectedId = search.get('source');
    const closeSheet = () => {
        const next = new URLSearchParams(search);
        next.delete('source');
        setSearch(next, { replace: true });
    };
    return (
        <PageLayout loading={loading}>
            <Stack spacing={3}>
                <Stack
                    direction="row"
                    sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 2, flexWrap: 'wrap' }}
                >
                    <Stack>
                        <Typography component="h1" variant="h5" style={{ fontWeight: 700 }}>
                            {label('serverToolsTitle', 'Server Tools')}
                        </Typography>
                        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                            {label(
                                'serverToolsHint',
                                'Provided and run by Tingly Box on the server. No MCP connection needed: they work as soon as they are on.'
                            )}
                        </Typography>
                    </Stack>
                    <IconButton
                        aria-label={label('refresh', 'Refresh connections')}
                        disabled={busy || checking}
                        onClick={() => void ws.run(ws.reload)}
                    >
                        <Refresh />
                    </IconButton>
                </Stack>
                <MCPWorkspaceAlerts ws={ws} />
                <MCPToolsPanel
                    sources={sources}
                    routes={(routing?.sources || []).filter((route) => sources.some((source) => source.id === route.id))}
                    loading={checking}
                    enabled
                    saveSource={ws.saveSource}
                    onConfigureSource={(id) => setSearch({ source: id })}
                />
            </Stack>
            <MCPSourceDrawer ws={ws} selectedId={selectedId} onClose={closeSheet} />
        </PageLayout>
    );
}

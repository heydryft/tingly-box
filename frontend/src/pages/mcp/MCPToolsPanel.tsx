import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Box, Button, Chip, CircularProgress, Stack, Typography } from '@mui/material';
import UnifiedCard from '@/components/UnifiedCard';
import MCPToolCard from './MCPToolCard';
import MCPToolTestDialog from './MCPToolTestDialog';
import { connectionSummary, isServerToolSource, isServerWebtoolsSource, toolPolicyPatch, type MCPToolPatch } from './toolPresentation';
import type { MCPCatalogTool, MCPRouteSource, MCPSourceConfig } from './types';

// Every connection and Server Tool in one list. The applied routing snapshot is
// the source of tool state, so what is shown is what the gateway will do.
export default function MCPToolsPanel({
    sources,
    routes,
    loading,
    enabled,
    saveSource,
    onConfigureSource,
}: {
    sources: MCPSourceConfig[];
    routes: MCPRouteSource[];
    loading: boolean;
    enabled: boolean;
    saveSource: (patch: MCPSourceConfig) => Promise<void>;
    onConfigureSource: (id: string) => void;
}) {
    const { t } = useTranslation();
    const workspace = (key: string, fallback: string) => t(`mcp.workspace.${key}`, { defaultValue: fallback });
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [testing, setTesting] = useState<MCPCatalogTool | null>(null);
    const policy = async (tool: MCPCatalogTool, patch: MCPToolPatch) => {
        const source = sources.find((s) => s.id === tool.source_id);
        if (!source) return;
        setBusy(true);
        setError('');
        try {
            await saveSource(toolPolicyPatch(source, tool, patch));
        } catch (e) {
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setBusy(false);
        }
    };
    return (
        <Stack spacing={2}>
            {error && <Alert severity="error">{error}</Alert>}
            {loading && <CircularProgress size={20} />}
            {!loading && sources.length === 0 && (
                <Typography color="text.secondary">
                    {workspace(
                        'emptyConnectionsHint',
                        'Paste a tool service URL or enter a local command. We will discover its tools for you.'
                    )}
                </Typography>
            )}
            {sources.map((source) => {
                const route = routes.find((r) => r.id === source.id);
                const tools = route?.tools || [];
                const advisor = source.transport === 'advisor' || !!source.advisor;
                const serverTool = isServerToolSource(source);
                const webtools = isServerWebtoolsSource(source);
                const summary = connectionSummary(source);
                const failed = route?.state === 'error' || route?.state === 'unconfigured';
                return (
                    <Box
                        component="article"
                        aria-label={source.name || source.id}
                        key={source.id}
                        sx={{ minWidth: 0 }}
                    >
                        <UnifiedCard
                            size="full"
                            title={source.name || source.id}
                            leftAction={
                                <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                                    <Typography variant="caption" color="text.secondary">
                                        {serverTool
                                            ? workspace('serverToolLabel', 'Server Tool')
                                            : t(`mcp.relationships.${route?.origin || source.origin || 'external'}`, {
                                                  defaultValue: route?.origin || source.origin || 'external',
                                              })}
                                    </Typography>
                                    <Chip
                                        size="small"
                                        variant="outlined"
                                        label={
                                            source.enabled === false
                                                ? workspace('off', 'Off')
                                                : !route || loading
                                                  ? workspace('checking', 'Checking…')
                                                  : route.state === 'connected'
                                                    ? workspace('connected', 'Connected')
                                                    : workspace('needsAttention', 'Needs attention')
                                        }
                                    />
                                </Stack>
                            }
                            rightAction={
                                <Button size="small" onClick={() => onConfigureSource(source.id!)}>
                                    {webtools
                                        ? workspace('webtoolsSettings', 'Web tool settings')
                                        : failed
                                          ? workspace('fixConnection', 'Fix connection')
                                          : advisor
                                            ? workspace('configureAdvisor', 'Configure Advisor model')
                                            : workspace('configureConnection', 'Configure this connection')}
                                </Button>
                            }
                            headerSx={{
                                flexDirection: { xs: 'column', sm: 'row' },
                                gap: 1,
                                '& > :first-child': { minWidth: 0, flexWrap: 'wrap' },
                                '& h2': { overflowWrap: 'anywhere' },
                            }}
                        >
                            <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap', mb: 1.5 }}>
                                <Typography
                                    variant="body2"
                                    color="text.secondary"
                                    sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}
                                >
                                    {summary.target || workspace('notSet', 'Not set')}
                                </Typography>
                                {summary.details.map(({ key, count }) => (
                                    <Chip
                                        key={key}
                                        size="small"
                                        variant="outlined"
                                        label={`${workspace(`detail_${key}`, key)}${key === 'proxy' ? '' : ` · ${count}`}`}
                                    />
                                ))}
                                {summary.missing.map((key) => (
                                    <Chip
                                        key={key}
                                        size="small"
                                        color="warning"
                                        clickable
                                        onClick={() => onConfigureSource(source.id!)}
                                        label={workspace(`missing_${key}`, `Needs ${key}`)}
                                    />
                                ))}
                            </Stack>
                            {route && route.state !== 'connected' && !!route.error && (
                                <Alert severity={failed ? 'warning' : 'info'} sx={{ mb: 1 }}>
                                    {route.error}
                                </Alert>
                            )}
                            {webtools && (
                                <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                                    {workspace('webtoolsContext', 'Configure Search with a Serper API key and enable Search / Fetch separately. Ready tools replace native web tools on supported model routes. The MCP version has independent settings.')}
                                </Typography>
                            )}
                            {advisor && (
                                <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                                    {workspace(
                                        'advisorContext',
                                        'Advisor is run by the gateway during model requests, so it has a switch and a consultation model but is not offered through profiles.'
                                    )}
                                </Typography>
                            )}
                            <Stack spacing={1}>
                                {tools.map((tool) => (
                                    <MCPToolCard
                                        key={tool.normalized_name}
                                        tool={tool}
                                        source={source}
                                        enabled={enabled}
                                        busy={busy || loading}
                                        onChange={(patch) => void policy(tool, patch)}
                                        onTest={() => setTesting(tool)}
                                    />
                                ))}
                                {!loading && route?.state === 'connected' && tools.length === 0 && (
                                    <Typography variant="body2" color="text.secondary">
                                        {workspace('emptyDiscovery', 'The connection returned no tools.')}
                                    </Typography>
                                )}
                            </Stack>
                        </UnifiedCard>
                    </Box>
                );
            })}
            {testing && (
                <MCPToolTestDialog
                    key={testing.normalized_name}
                    tool={
                        routes
                            .flatMap((r) => r.tools)
                            .find((tool) => tool.normalized_name === testing.normalized_name) || {
                            ...testing,
                            enabled: false,
                        }
                    }
                    enabled={enabled}
                    onClose={() => setTesting(null)}
                />
            )}
        </Stack>
    );
}

import React, {useEffect, useMemo, useState} from 'react';
import {useSelector} from 'react-redux';

import type {GlobalState} from '@mattermost/types/store';

import type {BotDefinition, BotRunResult, ExecutionRecord} from '../client';
import {getBots, getHistory, runBot} from '../client';

const card: React.CSSProperties = {background: 'rgba(var(--center-channel-color-rgb),.04)', border: '1px solid rgba(var(--center-channel-color-rgb),.12)', borderRadius: 12, padding: 12, display: 'flex', flexDirection: 'column', gap: 8};
const field: React.CSSProperties = {width: '100%', border: '1px solid rgba(var(--center-channel-color-rgb),.16)', borderRadius: 8, padding: '10px 12px'};

export default function RHSPane() {
    const channelId = useSelector((state: GlobalState) => state.entities.channels.currentChannelId);
    const selectedPostId = useSelector((state: GlobalState) => (state as any).views?.rhs?.selectedPostId as string | undefined);
    const post = useSelector((state: GlobalState) => selectedPostId ? ((state as any).entities?.posts?.posts || {})[selectedPostId] : null) as any;
    const files = useSelector((state: GlobalState) => ((state as any).entities?.files?.files || {})) as Record<string, any>;

    const [bots, setBots] = useState<BotDefinition[]>([]);
    const [history, setHistory] = useState<ExecutionRecord[]>([]);
    const [selectedBotId, setSelectedBotId] = useState('');
    const [prompt, setPrompt] = useState('');
    const [message, setMessage] = useState('');
    const [loading, setLoading] = useState(true);
    const [submitting, setSubmitting] = useState(false);
    const [lastResult, setLastResult] = useState<BotRunResult | null>(null);

    const bot = useMemo(() => bots.find((item) => item.id === selectedBotId) || bots[0] || null, [bots, selectedBotId]);
    const fileIds = Array.isArray(post?.file_ids) ? post.file_ids.filter(Boolean) : [];
    const fileNames = fileIds.map((id: string) => files[id]?.name || id);
    const rootId = post?.root_id || post?.id || selectedPostId;

    useEffect(() => {
        let cancelled = false;
        async function load() {
            setLoading(true);
            setMessage('');
            try {
                const [nextBots, nextHistory] = await Promise.all([getBots(channelId), getHistory(5)]);
                if (cancelled) {
                    return;
                }
                setBots(nextBots);
                setHistory(nextHistory);
                setSelectedBotId((current) => current && nextBots.some((item) => item.id === current) ? current : (nextBots[0]?.id || ''));
            } catch (e) {
                if (!cancelled) {
                    setMessage((e as Error).message);
                }
            } finally {
                if (!cancelled) {
                    setLoading(false);
                }
            }
        }
        void load();
        return () => {
            cancelled = true;
        };
    }, [channelId]);

    async function submit() {
        if (!bot || !channelId || (!prompt.trim() && fileIds.length === 0)) {
            return;
        }
        setSubmitting(true);
        setMessage('');
        try {
            const result = await runBot({bot_id: bot.id, channel_id: channelId, root_id: rootId, prompt, file_ids: fileIds});
            setLastResult(result);
            setPrompt('');
            setHistory(await getHistory(5));
            setMessage(`@${bot.username} \ubd07\uc774 Mattermost \uc2a4\ub808\ub4dc\uc5d0 \uc751\ub2f5\uc744 \uac8c\uc2dc\ud588\uc2b5\ub2c8\ub2e4.`);
        } catch (e) {
            setMessage((e as Error).message);
        } finally {
            setSubmitting(false);
        }
    }

    return (
        <div style={{display: 'flex', flexDirection: 'column', gap: 16, padding: 16}}>
            <section style={card}>
                <strong>{'Mattermost LLM'}</strong>
                <span style={{fontSize: 12, opacity: .8}}>{'\ud14d\uc2a4\ud2b8\ub9cc \ubcf4\ub0b4\ub3c4 \ub418\uace0, \ucca8\ubd80 \ud30c\uc77c\uc744 \ud568\uaed8 \ubcf4\ub0b4 \uba40\ud2f0\ubaa8\ub2ec / \ubb38\uc11c \ubd84\uc11d\uc744 \uc2dc\uc791\ud560 \uc218 \uc788\uc2b5\ub2c8\ub2e4.'}</span>
                {loading && <span>{'\ubd07 \ubaa9\ub85d\uc744 \ubd88\ub7ec\uc624\ub294 \uc911\uc785\ub2c8\ub2e4...'}</span>}
                {!loading && bots.length === 0 && <span>{'\ud604\uc7ac \ucc44\ub110\uc5d0\uc11c \uc0ac\uc6a9\ud560 \uc218 \uc788\ub294 \ubd07\uc774 \uc5c6\uc2b5\ub2c8\ub2e4.'}</span>}
                {!loading && bots.length > 0 && (
                    <>
                        <select style={field} value={bot?.id || ''} onChange={(e) => setSelectedBotId(e.target.value)}>
                            {bots.map((item) => <option key={item.id} value={item.id}>{`${item.display_name || item.username} (@${item.username})`}</option>)}
                        </select>
                        <div style={{fontSize: 12, opacity: .8}}>{selectedPostId ? (fileNames.length > 0 ? `\ucca8\ubd80 \ud30c\uc77c: ${fileNames.join(', ')}` : '\ucca8\ubd80 \ud30c\uc77c \uc5c6\uc774 \ud14d\uc2a4\ud2b8 \ub300\ud654\ub85c\ub3c4 \uc2e4\ud589\ud560 \uc218 \uc788\uc2b5\ub2c8\ub2e4.') : '\ud3ec\uc2a4\ud2b8\uc5d0\uc11c RHS\ub97c \uc5f4\uc5b4 \ud14d\uc2a4\ud2b8 \ub610\ub294 \ucca8\ubd80 \ud30c\uc77c \uae30\ubc18 \uc694\uccad\uc744 \ubcf4\ub0bc \uc218 \uc788\uc2b5\ub2c8\ub2e4.'}</div>
                        <textarea style={{...field, resize: 'vertical'}} rows={5} value={prompt} placeholder={'\uc608: \uc774 \ubb38\uc11c \ud575\uc2ec\uc744 5\uc904\ub85c \uc694\uc57d\ud574\uc918 / \uc624\ub298 \ud68c\uc758 \ub0b4\uc6a9\uc744 \uc815\ub9ac\ud574\uc918'} onChange={(e) => setPrompt(e.target.value)}/>
                        <button className='btn btn-primary' type='button' disabled={submitting || !bot || !channelId || (!prompt.trim() && fileIds.length === 0)} onClick={submit}>{submitting ? '\uc694\uccad \uc911...' : `@${bot?.username || 'bot'}\ub85c \uc2e4\ud589`}</button>
                    </>
                )}
                {message && <span>{message}</span>}
            </section>

            {lastResult && (
                <section style={card}>
                    <strong>{'\ucd5c\uadfc \uc2e4\ud589 \uacb0\uacfc'}</strong>
                    <div>{`${lastResult.bot_name || lastResult.bot_username} - ${lastResult.status}`}</div>
                    <div>{`Model: ${lastResult.model}`}</div>
                    {typeof lastResult.api_duration_ms === 'number' && lastResult.api_duration_ms > 0 && <div>{`API: ${(lastResult.api_duration_ms / 1000).toFixed(2)}s`}</div>}
                    {lastResult.output && <div style={{fontSize: 12, opacity: .8, whiteSpace: 'pre-wrap'}}>{cut(lastResult.output, 400)}</div>}
                    {lastResult.error_message && <div style={{whiteSpace: 'pre-wrap'}}>{lastResult.error_message}</div>}
                </section>
            )}

            <section style={card}>
                <strong>{'\ucd5c\uadfc \uae30\ub85d'}</strong>
                {history.length === 0 && <span>{'\uc544\uc9c1 \uc2e4\ud589 \uae30\ub85d\uc774 \uc5c6\uc2b5\ub2c8\ub2e4.'}</span>}
                {history.map((item) => <div key={item.correlation_id} style={{fontSize: 12}}><strong>{item.bot_name || item.bot_username}</strong><div>{`@${item.bot_username} -> ${item.model}`}</div><div>{`${item.status} via ${item.source}`}</div>{item.error_message && <div style={{whiteSpace: 'pre-wrap'}}>{item.error_message}</div>}</div>)}
            </section>
        </div>
    );
}

function cut(value: string, max: number) {
    const next = (value || '').trim();
    return next.length <= max ? next : `${next.slice(0, max - 1)}...`;
}

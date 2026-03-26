import React, {useEffect, useMemo, useRef, useState} from 'react';

import type {WebSocketMessage} from '@mattermost/client';

import PostText from './post_text';

import {isDoc2VLLMAwaitingFirstChunk} from '../streaming';

type PostUpdateData = {
    post_id?: string;
    next?: string;
    control?: string;
};

type Props = {
    post: any;
    websocketRegister: (postID: string, listenerID: string, listener: (msg: WebSocketMessage<PostUpdateData>) => void) => void;
    websocketUnregister: (postID: string, listenerID: string) => void;
};

const containerStyle: React.CSSProperties = {display: 'flex', flexDirection: 'column', gap: '8px'};
const statusStyle: React.CSSProperties = {color: 'rgba(var(--center-channel-color-rgb), 0.72)', fontSize: '12px', fontWeight: 600, letterSpacing: '0.01em'};
const precontentStyle: React.CSSProperties = {alignItems: 'center', color: 'rgba(var(--center-channel-color-rgb), 0.72)', display: 'inline-flex', fontSize: '13px', gap: '8px'};
const spinnerStyle: React.CSSProperties = {animation: 'doc2vllm-stream-cursor-blink 700ms linear infinite', background: 'rgba(var(--center-channel-color-rgb), 0.16)', borderRadius: '999px', display: 'inline-block', height: '10px', width: '10px'};
const toolbarStyle: React.CSSProperties = {alignItems: 'center', display: 'flex', gap: '8px'};
const buttonStyle: React.CSSProperties = {background: 'rgba(var(--button-bg-rgb), 0.12)', border: '1px solid rgba(var(--button-bg-rgb), 0.28)', borderRadius: '999px', color: 'rgb(var(--button-bg-rgb))', cursor: 'pointer', fontSize: '12px', fontWeight: 600, padding: '6px 12px'};
const debugDrawerStyle: React.CSSProperties = {background: 'var(--center-channel-bg)', border: '1px solid rgba(var(--center-channel-color-rgb), 0.12)', borderRadius: '12px', boxShadow: '0 8px 24px rgba(0, 0, 0, 0.12)', color: 'rgb(var(--center-channel-color-rgb))', display: 'flex', flexDirection: 'column', overflow: 'hidden'};
const debugHeaderStyle: React.CSSProperties = {alignItems: 'center', borderBottom: '1px solid rgba(var(--center-channel-color-rgb), 0.12)', display: 'flex', justifyContent: 'space-between', gap: '12px', padding: '12px 16px'};
const debugBodyStyle: React.CSSProperties = {display: 'grid', gap: '12px', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', padding: '16px'};
const debugPanelStyle: React.CSSProperties = {background: 'rgba(var(--center-channel-color-rgb), 0.04)', border: '1px solid rgba(var(--center-channel-color-rgb), 0.08)', borderRadius: '10px', display: 'flex', flexDirection: 'column', gap: '12px', minHeight: 0, padding: '16px'};
const debugPreStyle: React.CSSProperties = {background: 'rgba(var(--center-channel-color-rgb), 0.04)', borderRadius: '8px', fontSize: '12px', margin: 0, maxHeight: '40vh', minHeight: '160px', overflow: 'auto', padding: '12px', whiteSpace: 'pre-wrap', wordBreak: 'break-word'};

export default function Doc2VLLMBotPost(props: Props) {
    const [message, setMessage] = useState(getRenderableMessage(props.post));
    const [generating, setGenerating] = useState(isStreamingPost(props.post));
    const [precontent, setPrecontent] = useState(isDoc2VLLMAwaitingFirstChunk(props.post));
    const [showDebugModal, setShowDebugModal] = useState(false);
    const listenerID = useRef(`doc2vllm-${Math.random().toString(36).slice(2)}`);
    const inputDebug = normalizeDebugPayload(props.post?.props?.doc2vllm_request_input || props.post?.props?.doc2vllm_error_input);
    const outputDebug = normalizeDebugPayload(props.post?.props?.doc2vllm_response_output || props.post?.props?.doc2vllm_error_output);
    const canShowDebug = inputDebug !== '' || outputDebug !== '';
    const debugButtonLabel = showDebugModal ? '\ud30c\ub77c\ubbf8\ud130 \uc228\uae30\uae30' : (outputDebug !== '' ? '\uc694\uccad/\uc751\ub2f5 \ud30c\ub77c\ubbf8\ud130 \ubcf4\uae30' : '\uc694\uccad \ud30c\ub77c\ubbf8\ud130 \ubcf4\uae30');
    const debugModalTitle = outputDebug !== '' ? 'Doc2VLLM \uc694\uccad/\uc751\ub2f5 \ud30c\ub77c\ubbf8\ud130' : 'Doc2VLLM \uc694\uccad \ud30c\ub77c\ubbf8\ud130';

    useEffect(() => {
        setMessage(getRenderableMessage(props.post));
        setGenerating(isStreamingPost(props.post));
        setPrecontent(isDoc2VLLMAwaitingFirstChunk(props.post));
        setShowDebugModal(false);
    }, [props.post.id, props.post.message, props.post.props?.doc2vllm_streaming, props.post.props?.doc2vllm_stream_status, props.post.props?.doc2vllm_stream_placeholder, props.post.props?.doc2vllm_request_input, props.post.props?.doc2vllm_response_output, props.post.props?.doc2vllm_error_input, props.post.props?.doc2vllm_error_output]);

    const listener = useMemo(() => ((msg: WebSocketMessage<PostUpdateData>) => {
        const data = msg?.data || {};
        if (data.post_id !== props.post.id) {
            return;
        }
        if (data.control === 'start') {
            setGenerating(true);
            setPrecontent(true);
            setMessage('');
            return;
        }
        if (typeof data.next === 'string' && data.next !== '') {
            setGenerating(true);
            setPrecontent(false);
            setMessage(data.next);
            return;
        }
        if (data.control === 'end' || data.control === 'cancel') {
            setGenerating(false);
            setPrecontent(false);
        }
    }), [props.post.id]);

    useEffect(() => {
        props.websocketRegister(props.post.id, listenerID.current, listener);
        return () => props.websocketUnregister(props.post.id, listenerID.current);
    }, [listener, props.post.id, props.websocketRegister, props.websocketUnregister]);

    return (
        <div data-testid='doc2vllm-bot-post' style={containerStyle}>
            {canShowDebug && <div style={toolbarStyle}><button style={buttonStyle} type='button' onClick={() => setShowDebugModal((open) => !open)}>{debugButtonLabel}</button></div>}
            {showDebugModal && canShowDebug && (
                <div style={debugDrawerStyle}>
                    <div style={debugHeaderStyle}>
                        <div style={{display: 'flex', flexDirection: 'column', gap: '4px'}}>
                            <strong>{debugModalTitle}</strong>
                            <span style={statusStyle}>{`Correlation ID: ${props.post?.props?.doc2vllm_correlation_id || '-'}`}</span>
                        </div>
                        <button style={buttonStyle} type='button' onClick={() => setShowDebugModal(false)}>{'\ub2eb\uae30'}</button>
                    </div>
                    <div style={debugBodyStyle}>
                        <section style={debugPanelStyle}>
                            <strong>{'\uc694\uccad \ud30c\ub77c\ubbf8\ud130'}</strong>
                            <pre style={debugPreStyle}>{inputDebug || '{}'}</pre>
                        </section>
                        {outputDebug !== '' && (
                            <section style={debugPanelStyle}>
                                <strong>{'\uc751\ub2f5 \ud30c\ub77c\ubbf8\ud130'}</strong>
                                <pre style={debugPreStyle}>{outputDebug}</pre>
                            </section>
                        )}
                    </div>
                </div>
            )}
            {precontent && <span style={precontentStyle}><span style={spinnerStyle}/>{'\uc751\ub2f5 \uc0dd\uc131 \uc2dc\uc791 \uc911...'}</span>}
            <PostText channelID={props.post.channel_id} message={message} postID={props.post.id} showCursor={generating && !precontent}/>
            {generating && !precontent && <span style={statusStyle}>{'\uc751\ub2f5 \uc0dd\uc131 \uc911...'}</span>}
        </div>
    );
}

function isStreamingPost(post: any) {
    return post?.props?.doc2vllm_streaming === 'true' || post?.props?.doc2vllm_stream_status === 'streaming';
}

function getRenderableMessage(post: any) {
    if (isDoc2VLLMAwaitingFirstChunk(post)) {
        return '';
    }
    return post?.message || '';
}

function normalizeDebugPayload(value: unknown) {
    if (typeof value !== 'string') {
        return '';
    }
    return value.trim();
}

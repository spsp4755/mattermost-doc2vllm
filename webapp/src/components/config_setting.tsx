import manifest from 'manifest';
import React, {useEffect, useMemo, useRef, useState} from 'react';

import type {
    AdminPluginConfig,
    BotDefinition,
    ConnectionStatus,
    ManagedBotStatus,
    PluginStatus,
} from '../client';
import {getAdminConfig, getStatus, testConnection} from '../client';

const defaultURL = 'http://localhost:8000/v1/chat/completions';
const defaultModel = 'Qwen/Qwen2.5-7B-Instruct';
const defaultMultimodalModel = 'Qwen/Qwen2.5-VL-7B-Instruct';

const stack: React.CSSProperties = {display: 'flex', flexDirection: 'column', gap: 16};
const card: React.CSSProperties = {background: 'white', border: '1px solid rgba(63,67,80,.12)', borderRadius: 8, padding: 20, display: 'flex', flexDirection: 'column', gap: 12};
const row2: React.CSSProperties = {display: 'grid', gridTemplateColumns: 'repeat(2,minmax(0,1fr))', gap: 12};
const row3: React.CSSProperties = {display: 'grid', gridTemplateColumns: 'repeat(3,minmax(0,1fr))', gap: 12};
const botLayout: React.CSSProperties = {display: 'grid', gridTemplateColumns: '280px minmax(0,1fr)', gap: 16};
const field: React.CSSProperties = {width: '100%', border: '1px solid rgba(63,67,80,.16)', borderRadius: 8, padding: '10px 12px'};
const note: React.CSSProperties = {fontSize: 12, opacity: 0.75, lineHeight: 1.5};
const box: React.CSSProperties = {padding: 12, borderRadius: 8, background: 'rgba(var(--button-bg-rgb),.08)', border: '1px solid rgba(var(--button-bg-rgb),.18)'};
const botButton: React.CSSProperties = {padding: 12, borderRadius: 8, background: 'rgba(var(--center-channel-color-rgb),.03)', border: '1px solid rgba(var(--center-channel-color-rgb),.08)', textAlign: 'left', display: 'flex', flexDirection: 'column', gap: 4};
const codeStyle: React.CSSProperties = {margin: 0, fontSize: 12, lineHeight: 1.5, background: 'rgba(var(--center-channel-color-rgb),.04)', borderRadius: 8, padding: 12, overflowX: 'auto', whiteSpace: 'pre-wrap'};

const T = {
    title: '\uad00\ub9ac\uc790 \uc124\uc815',
    intro: '\ud14d\uc2a4\ud2b8 \ub300\ud654, \uba40\ud2f0\ubaa8\ub2ec \ubd84\uc11d, \ubb38\uc11c \ucd94\ucd9c\uc744 \ud55c \ud50c\ub7ec\uadf8\uc778\uc5d0\uc11c \uad00\ub9ac\ud560 \uc218 \uc788\ub3c4\ub85d \ubc94\uc6a9 LLM \uc124\uc815 \ud654\uba74\uc73c\ub85c \uc815\ub9ac\ud588\uc2b5\ub2c8\ub2e4.',
    botTip1: '\uc0c8 \ubd07\uc744 \ucd94\uac00\ud558\uba74 \ube48 \uc785\ub825 \uc0c1\ud0dc\ub85c \uc2dc\uc791\ud558\ubbc0\ub85c \ud544\uc694\ud55c \uac12\ub9cc \uc9c1\uc811 \uc785\ub825\ud558\uba74 \ub429\ub2c8\ub2e4.',
    botTip2: '\uc22b\uc790 \ud30c\ub77c\ubbf8\ud130\ub294 \ubaa8\ub450 \uc9c1\uc811 \uc785\ub825\ud558\ub294 \ud615\ud0dc\ub85c \uc720\uc9c0\ud588\uc2b5\ub2c8\ub2e4.',
    botTip3: '\uc124\uc815 \uc800\uc7a5 \ud6c4 \uc0c1\ud0dc \uc0c8\ub85c\uace0\uce68\uc744 \ub204\ub974\uba74 Mattermost \ubd07 \uacc4\uc815 \ub4f1\ub85d \uc5ec\ubd80\ub97c \ud655\uc778\ud560 \uc218 \uc788\uc2b5\ub2c8\ub2e4.',
    source: '\ubd88\ub7ec\uc628 \uc124\uc815 \ucd9c\ucc98',
    readonly: '\uc774 \uc124\uc815\uc740 \ud658\uacbd \ubcc0\uc218\ub85c \uad00\ub9ac\ub418\uace0 \uc788\uc5b4 \uc5ec\uae30\uc11c\ub294 \uc77d\uae30 \uc804\uc6a9\uc785\ub2c8\ub2e4.',
    service: '\uc11c\ube44\uc2a4 \uc5f0\uacb0',
    loadingConfig: '\uc124\uc815\uc744 \ubd88\ub7ec\uc624\ub294 \uc911\uc785\ub2c8\ub2e4...',
    baseUrl: '\uae30\ubcf8 URL',
    baseUrlHelp: '\ub8e8\ud2b8 URL, /v1, /v1/chat/completions \uc911 \uc5b4\ub290 \ud615\ud0dc\ub85c \uc785\ub825\ud574\ub3c4 \uc790\ub3d9 \uc815\uaddc\ud654\ub429\ub2c8\ub2e4.',
    authMode: '\uc778\uc99d \ubc29\uc2dd',
    apiKey: '\uae30\ubcf8 API \ud0a4',
    allowHosts: '\ud5c8\uc6a9 \ud638\uc2a4\ud2b8',
    allowHostsHelp: '\ube44\uc6cc\ub450\uba74 \uae30\ubcf8 URL, \ubd07 \uc804\uc6a9 URL, \ud6c4\ucc98\ub9ac URL\uc758 \ud638\uc2a4\ud2b8\uac00 \uc790\ub3d9\uc73c\ub85c \ud5c8\uc6a9\ub429\ub2c8\ub2e4.',
    timeout: '\ud0c0\uc784\uc544\uc6c3(\ucd08)',
    enableStreaming: '가능하면 streaming 사용',
    enableStreamingHelp: '지원하는 모델은 실시간으로 답변을 갱신하고, 지원하지 않으면 자동으로 일반 응답으로 전환합니다.',
    streamingUpdateMs: 'Streaming 업데이트 간격(ms)',
    maxInput: '\ucd5c\ub300 \uc785\ub825 \uae38\uc774',
    maxOutput: '\ucd5c\ub300 \ucd9c\ub825 \uae38\uc774',
    pdfDpi: 'PDF DPI',
    maxPdfPages: '\ucd5c\ub300 PDF \ud398\uc774\uc9c0 \uc218',
    directInput: '\uc9c1\uc811 \uc22b\uc790\ub97c \uc785\ub825\ud558\uc138\uc694.',
    maskDefault: '\uae30\ubcf8 \ubbfc\uac10\uc815\ubcf4 \ub9c8\uc2a4\ud0b9 \uc0ac\uc6a9',
    debugLogs: '\ub514\ubc84\uadf8 \ub85c\uadf8 \ud65c\uc131\ud654',
    usageLogs: '\uc0ac\uc6a9\ub7c9 \ub85c\uadf8 \ud65c\uc131\ud654',
    bots: '\ubd07 \uce74\ud0c8\ub85c\uadf8',
    loadSamples: '\uc608\uc2dc \ubd88\ub7ec\uc624\uae30',
    addBot: '\uc0c8 \ubd07 \ucd94\uac00',
    addHint: '\uc0c8 \ubd07\uc740 \ube48 \ucd08\uae30\uc785\ub825\uc73c\ub85c \ucd94\uac00\ub429\ub2c8\ub2e4. * \ud45c\uc2dc \ud56d\ubaa9\uc740 \ud544\uc218\ub85c \ucc44\uc6cc \uc8fc\uc138\uc694.',
    noBots: '\uc544\uc9c1 \ub4f1\ub85d\ub41c \ubd07\uc774 \uc5c6\uc2b5\ub2c8\ub2e4.',
    selectBot: '\uc67c\ucabd\uc5d0\uc11c \ubd07\uc744 \uc120\ud0dd\ud558\uc138\uc694.',
    duplicate: '\ubcf5\uc81c',
    delete: '\uc0ad\uc81c',
    username: 'username',
    displayName: '\ud45c\uc2dc \uc774\ub984',
    description: '\uc124\uba85',
    internalId: '\ub0b4\ubd80 ID',
    internalIdHelp: 'username\uc744 \uae30\uc900\uc73c\ub85c \uc790\ub3d9 \uad00\ub9ac\ub429\ub2c8\ub2e4.',
    model: '\ubaa8\ub378\uba85',
    mode: '\uc791\ub3d9 \ubc29\uc2dd',
    chatMode: '\ud14d\uc2a4\ud2b8 \uc0dd\uc131 / \uc77c\ubc18 \ub300\ud654',
    ocrMode: 'OCR / \ucd94\ucd9c \uc911\uc2ec',
    multimodalMode: '\uba40\ud2f0\ubaa8\ub2ec / \uc2dc\uac01\uc5b8\uc5b4 \ubaa8\ub378',
    outputMode: '\ucd9c\ub825 \ud615\uc2dd',
    systemPrompt: 'System Prompt / \uc9c0\uc2dc \ud504\ub86c\ud504\ud2b8',
    extraJson: '\ucd94\uac00 \uc694\uccad \ud30c\ub77c\ubbf8\ud130(JSON)',
    botBaseUrl: '\ubd07 \uc804\uc6a9 URL',
    botBaseUrlHelp: '\ube44\uc6cc\ub450\uba74 \uae30\ubcf8 URL\uc744 \uadf8\ub300\ub85c \uc0ac\uc6a9\ud569\ub2c8\ub2e4.',
    botApiKey: '\ubd07 \uc804\uc6a9 API \ud0a4',
    botAuthMode: '\ubd07 \uc804\uc6a9 \uc778\uc99d \ubc29\uc2dd',
    useGlobal: '\uae30\ubcf8\uac12 \uc0ac\uc6a9',
    maskBot: '\uc774 \ubd07\uc5d0 \ubbfc\uac10\uc815\ubcf4 \ub9c8\uc2a4\ud0b9 \uc801\uc6a9',
    refiner: '\ub300\ud615 \ubaa8\ub378 \ud6c4\ucc98\ub9ac / QA \ubcf4\uac15',
    refinerUrl: '\ud6c4\ucc98\ub9ac URL',
    refinerUrlHelp: 'URL\uacfc \ubaa8\ub378\uba85\uc744 \ud568\uaed8 \uc785\ub825\ud558\uba74 \ud6c4\ucc98\ub9ac \ub2e8\uacc4\ub97c \ud65c\uc131\ud654\ud569\ub2c8\ub2e4.',
    refinerKey: '\ud6c4\ucc98\ub9ac API \ud0a4',
    refinerModel: '\ud6c4\ucc98\ub9ac \ubaa8\ub378',
    refinerScope: '\uc801\uc6a9 \ubc94\uc704',
    refinerPrompt: '\ud6c4\ucc98\ub9ac Prompt',
    initialOcr: '\ucd08\uae30 OCR \uacb0\uacfc\ub9cc',
    followupsOnly: '\ud6c4\uc18d \ub300\ud654\ub9cc',
    both: '\ucd08\uae30 OCR + \ud6c4\uc18d \ub300\ud654',
    allowedTeams: '\ud5c8\uc6a9 \ud300',
    allowedChannels: '\ud5c8\uc6a9 \ucc44\ub110',
    allowedUsers: '\ud5c8\uc6a9 \uc0ac\uc6a9\uc790',
    effectiveSettings: '\ud604\uc7ac \ubd07\uc758 \uc2e4\uc81c \uc801\uc6a9\uac12',
    effectiveUrl: '\uc2e4\uc81c \uc694\uccad URL',
    effectiveAuth: '\uc2e4\uc81c \uc778\uc99d \ubc29\uc2dd',
    effectiveModel: '\uc2e4\uc81c \ubaa8\ub378\uba85',
    effectiveRefiner: '\ud6c4\ucc98\ub9ac \uc0ac\uc6a9',
    inactive: '\uc0ac\uc6a9 \uc548 \ud568',
    validationTitle: '\uc800\uc7a5 \uc804 \ud655\uc778\ud560 \ud56d\ubaa9',
    status: '\uc5f0\uacb0 \ubc0f \ubd07 \ub3d9\uae30\ud654 \uc0c1\ud0dc',
    refreshStatus: '\uc0c1\ud0dc \uc0c8\ub85c\uace0\uce68',
    loadingStatus: '\uc0c1\ud0dc \ud655\uc778 \uc911...',
    testConnection: '\uc120\ud0dd\ud55c \ubd07 \uc5f0\uacb0 \ud14c\uc2a4\ud2b8',
    testConnectionHelp: '\ud604\uc7ac \ud3b8\uc9d1 \uc911\uc778 \uac12 \uae30\uc900\uc73c\ub85c, \uc9c0\uae08 \uc120\ud0dd\ud55c \ubd07\uc758 URL\u00b7\uc778\uc99d\u00b7\ubaa8\ub378 \uc870\ud569\uc744 \ubc14\ub85c \ud655\uc778\ud569\ub2c8\ub2e4.',
    testing: '\uc5f0\uacb0 \ud655\uc778 \uc911...',
    pluginId: '\ud50c\ub7ec\uadf8\uc778 ID',
    configuredBots: '\uc124\uc815\ub41c \ubd07 \uc218',
    managedBots: '\uad00\ub9ac\ub418\ub294 Mattermost \ubd07 \uc0c1\ud0dc',
    noManagedBots: '\uc544\uc9c1 \ub4f1\ub85d\ub41c \ubd07 \uc0c1\ud0dc\uac00 \uc5c6\uc2b5\ub2c8\ub2e4.',
    pdfSupport: 'PDF 처리 지원 상태',
    pdfTextExtractor: 'PDF 텍스트 추출기',
    pdfRasterizer: 'PDF 이미지 변환기',
    unavailable: '없음',
    preview: '\uc800\uc7a5\ub420 JSON \ubbf8\ub9ac\ubcf4\uae30',
    previewHelp: 'Mattermost \ud50c\ub7ec\uadf8\uc778 \uc124\uc815\uc5d0 \uc800\uc7a5\ub420 JSON \ubbf8\ub9ac\ubcf4\uae30\uc785\ub2c8\ub2e4.',
    showPreview: '\ubbf8\ub9ac\ubcf4\uae30 \uc5f4\uae30',
    hidePreview: '\ubbf8\ub9ac\ubcf4\uae30 \uc811\uae30',
    requiredGuide: '* \ud45c\uc2dc\ub294 \ud544\uc218 \uc785\ub825 \ud56d\ubaa9\uc785\ub2c8\ub2e4.',
    usernameHelp: '\ubd07 \ud638\ucd9c\uc6a9 username\uc785\ub2c8\ub2e4. \uacf5\ubc31\uc740 -\ub85c \ubcc0\ud658\ub429\ub2c8\ub2e4.',
    displayNameHelp: '\ube44\uc6cc \ub450\uba74 Mattermost\uc5d0\uc11c \uae30\ubcf8 \ud45c\uc2dc \uaddc\uce59\uc744 \ub530\ub985\ub2c8\ub2e4.',
    modelHelp: '\ube44\uc6cc \ub450\uba74 \ud50c\ub7ec\uadf8\uc778 \uae30\ubcf8 \ubaa8\ub378(Qwen/Qwen2.5-7B-Instruct)\uc744 \uc0ac\uc6a9\ud569\ub2c8\ub2e4.',
};

type DraftBot = {
    local_id: string;
    bot_id: string;
    username: string;
    display_name: string;
    description: string;
    base_url: string;
    auth_mode: string;
    auth_token: string;
    model: string;
    mode: string;
    output_mode: string;
    ocr_prompt: string;
    temperature: number;
    max_tokens: number;
    top_p: number;
    repetition_penalty: number;
    presence_penalty: number;
    frequency_penalty: number;
    extra_request_json: string;
    mask_sensitive_data: boolean;
    vllm_base_url: string;
    vllm_api_key: string;
    vllm_model: string;
    vllm_prompt: string;
    vllm_scope: string;
    allowed_teams: string[];
    allowed_channels: string[];
    allowed_users: string[];
};

type DraftConfig = {
    service: {base_url: string; auth_mode: string; auth_token: string; allow_hosts: string};
    runtime: {default_timeout_seconds: number; enable_streaming: boolean; streaming_update_ms: number; max_input_length: number; max_output_length: number; pdf_raster_dpi: number; max_pdf_pages: number; mask_sensitive_data: boolean; enable_debug_logs: boolean; enable_usage_logs: boolean};
    bots: DraftBot[];
};

type Props = {
    id?: string;
    value?: unknown;
    disabled?: boolean;
    setByEnv?: boolean;
    helpText?: React.ReactNode;
    onChange: (id: string, value: unknown) => void;
    setSaveNeeded?: () => void;
};

type FieldProps = {label: string; help?: string; required?: boolean; children: React.ReactNode};

const sampleBots: Partial<BotDefinition>[] = [
    {id: 'mm-llm-chat', username: 'mm-llm-chat', display_name: '\uc77c\ubc18 \ub300\ud654 / \ud14d\uc2a4\ud2b8 \uc0dd\uc131', description: '\ud14d\uc2a4\ud2b8 \uc9c8\uc758\uc640 \ubb38\uc11c \uc694\uc57d\uc5d0 \uc801\ud569\ud55c \ubc94\uc6a9 \ubd07', model: defaultModel, mode: 'chat', output_mode: 'markdown', ocr_prompt: '\uc0ac\uc6a9\uc790 \uc694\uccad\uc744 \uba85\ud655\ud558\uac8c \uc218\ud589\ud558\uace0, \ud575\uc2ec \uc704\uc8fc\ub85c \uac04\uacb0\ud558\uac8c \ub2f5\ud558\uc138\uc694.', temperature: 0.2, max_tokens: 2048, top_p: 1, repetition_penalty: 1, mask_sensitive_data: false},
    {id: 'mm-llm-qwen-vl', username: 'mm-llm-qwen-vl', display_name: '\uba40\ud2f0\ubaa8\ub2ec \ubd84\uc11d', description: 'Qwen \uacc4\uc5f4 \uba40\ud2f0\ubaa8\ub2ec \ubaa8\ub378 \uc608\uc2dc', model: defaultMultimodalModel, mode: 'multimodal', output_mode: 'markdown', ocr_prompt: '\ucca8\ubd80\ub41c \uc774\ubbf8\uc9c0\ub098 \ubb38\uc11c\uc758 \ubcf4\uc774\ub294 \ub0b4\uc6a9\ub9cc \uadfc\uac70\ub85c \ub2f5\ud558\uc138\uc694.', temperature: 0, max_tokens: 3072, top_p: 1, repetition_penalty: 1, extra_request_json: '{"min_pixels":3136,"max_pixels":12845056}', mask_sensitive_data: false},
    {id: 'mm-llm-ocr', username: 'mm-llm-ocr', display_name: '\ubb38\uc11c OCR / \ucd94\ucd9c', description: '\uc6d0\ubb38 \ucda9\uc2e4\ub3c4\uac00 \uc911\uc694\ud55c \ubb38\uc11c \ucd94\ucd9c\uc6a9 \ubd07', model: defaultMultimodalModel, mode: 'ocr', output_mode: 'markdown', ocr_prompt: '\ucca8\ubd80\ub41c \ubb38\uc11c\uc758 \ud14d\uc2a4\ud2b8\ub97c \uc6d0\ubb38\uc5d0 \ucda9\uc2e4\ud558\uac8c \ucd94\ucd9c\ud558\uc138\uc694.', temperature: 0, max_tokens: 2048, top_p: 1, repetition_penalty: 1, mask_sensitive_data: false},
];

export default function ConfigSetting(props: Props) {
    const key = props.id || 'Config';
    const disabled = Boolean(props.disabled || props.setByEnv);
    const [config, setConfig] = useState<DraftConfig>(createDefaultConfig());
    const [selected, setSelected] = useState('');
    const [showPreview, setShowPreview] = useState(false);
    const [status, setStatus] = useState<PluginStatus | null>(null);
    const [connection, setConnection] = useState<ConnectionStatus | null>(null);
    const [connectionError, setConnectionError] = useState('');
    const [source, setSource] = useState('config');
    const [error, setError] = useState('');
    const [loadingConfig, setLoadingConfig] = useState(true);
    const [loadingStatus, setLoadingStatus] = useState(true);
    const [testing, setTesting] = useState(false);
    const last = useRef('');
    const saveTimer = useRef<number | null>(null);
    const [deferredConfig, setDeferredConfig] = useState(config);

    useEffect(() => {
        void loadConfig(props.value, last, setConfig, setSource, setSelected, setLoadingConfig, setError);
    }, [props.value]);

    useEffect(() => {
        void loadStatus(setStatus, setLoadingStatus, setError);
    }, []);

    useEffect(() => {
        return () => {
            if (saveTimer.current != null) {
                window.clearTimeout(saveTimer.current);
            }
        };
    }, []);

    useEffect(() => {
        const timer = window.setTimeout(() => {
            setDeferredConfig(config);
        }, 75);

        return () => window.clearTimeout(timer);
    }, [config]);

    const bot = useMemo(() => config.bots.find((item) => selectionKey(item) === selected) || config.bots[0] || null, [config.bots, selected]);
    const messages = useMemo(() => validate(deferredConfig), [deferredConfig]);
    const preview = useMemo(() => showPreview ? serialize(buildConfig(deferredConfig)) : '', [deferredConfig, showPreview]);
    const managedBots = status?.managed_bots || status?.bot_sync?.entries || [];

    const apply = (next: DraftConfig, nextSelected?: string) => {
        setConfig(next);
        const raw = serialize(buildConfig(next));
        last.current = raw;
        if (saveTimer.current != null) {
            window.clearTimeout(saveTimer.current);
        }
        saveTimer.current = window.setTimeout(() => {
            props.onChange(key, raw);
            props.setSaveNeeded?.();
            saveTimer.current = null;
        }, 100);
        setSelected(nextSelected || pickBot(next.bots, selected));
    };

    const updateService = (patch: Partial<DraftConfig['service']>) => apply({...config, service: {...config.service, ...patch}});
    const updateRuntime = (patch: Partial<DraftConfig['runtime']>) => apply({...config, runtime: {...config.runtime, ...patch}});
    const updateBot = (botId: string, patch: Partial<DraftBot>) => {
        const bots = config.bots.map((item) => item.local_id === botId ? {...item, ...patch} : item);
        const nextBot = bots.find((item) => item.local_id === botId);
        apply({...config, bots}, nextBot ? selectionKey(nextBot) : undefined);
    };
    const addBot = () => {
        const next = emptyBot(config.bots, config.runtime.mask_sensitive_data);
        apply({...config, bots: [...config.bots, next]}, selectionKey(next));
    };
    const loadSamples = () => {
        const bots = sampleBots.map((item, index) => normalizeBot(item, index, config.runtime.mask_sensitive_data));
        apply({...config, bots}, bots[0] ? selectionKey(bots[0]) : '');
    };
    const duplicateBot = (current: DraftBot) => {
        const identity = nextBotIdentity(config.bots);
        const next: DraftBot = {...current, local_id: id('bot'), bot_id: identity.id, username: identity.username, display_name: `${current.display_name || 'Bot'} Copy`, allowed_teams: [...current.allowed_teams], allowed_channels: [...current.allowed_channels], allowed_users: [...current.allowed_users]};
        apply({...config, bots: [...config.bots, next]}, selectionKey(next));
    };
    const removeBot = (botId: string) => apply({...config, bots: config.bots.filter((item) => item.local_id !== botId)});
    const updateUsername = (current: DraftBot, rawValue: string) => {
        const nextUsername = draftUsername(rawValue);
        updateBot(current.local_id, {username: nextUsername, bot_id: idValue(nextUsername || current.bot_id, current.local_id)});
    };
    const refreshStatus = async () => {
        setLoadingStatus(true);
        setError('');
        try {
            setStatus(await getStatus());
        } catch (e) {
            setError((e as Error).message);
        } finally {
            setLoadingStatus(false);
        }
    };
    const runConnectionTest = async () => {
        setTesting(true);
        setConnection(null);
        setConnectionError('');
        try {
            setConnection(await testConnection(bot?.bot_id || bot?.username || undefined, buildConfig(config)));
        } catch (e) {
            setConnectionError((e as Error).message);
        } finally {
            setTesting(false);
        }
    };

    return (
        <div style={stack}>
            <section style={card}>
                <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center'}}>
                    <strong>{T.title}</strong>
                    <span style={{fontSize: 12, fontWeight: 700}}>{manifest.version}</span>
                </div>
                <span style={note}>{T.intro}</span>
                <div style={box}>
                    <div>{T.botTip1}</div>
                    <div>{T.botTip2}</div>
                    <div>{T.botTip3}</div>
                </div>
                {source !== 'config' && <div style={box}>{`${T.source}: ${source}`}</div>}
                {props.setByEnv && <div style={box}>{T.readonly}</div>}
                {props.helpText}
                {error && <div style={box}>{error}</div>}
                {messages.length > 0 && (
                    <div style={box}>
                        <strong>{T.validationTitle}</strong>
                        {messages.map((message) => <div key={message}>{message}</div>)}
                    </div>
                )}
                <span style={note}>{T.requiredGuide}</span>
            </section>

            <section style={card}>
                <strong>{T.service}</strong>
                {loadingConfig ? <span>{T.loadingConfig}</span> : (
                    <>
                        <div style={row2}>
                            <Field label={T.baseUrl} required={true} help={T.baseUrlHelp}><input disabled={disabled} style={field} value={config.service.base_url} placeholder={defaultURL} onChange={(e) => updateService({base_url: e.target.value})}/></Field>
                            <Field label={T.authMode}>
                                <select disabled={disabled} style={field} value={config.service.auth_mode} onChange={(e) => updateService({auth_mode: auth(e.target.value)})}>
                                    <option value='bearer'>{'Authorization: Bearer'}</option>
                                    <option value='x-api-key'>{'x-api-key'}</option>
                                </select>
                            </Field>
                        </div>
                        <div style={row2}>
                            <Field label={T.apiKey}><input disabled={disabled} type='password' style={field} value={config.service.auth_token} onChange={(e) => updateService({auth_token: e.target.value})}/></Field>
                            <Field label={T.allowHosts} help={T.allowHostsHelp}><input disabled={disabled} style={field} value={config.service.allow_hosts} placeholder={'localhost, *.internal.example.com'} onChange={(e) => updateService({allow_hosts: e.target.value})}/></Field>
                        </div>
                        <div style={row3}>
                            <Field label={T.timeout} help={T.directInput}><input disabled={disabled} type='number' min={1} style={field} value={String(config.runtime.default_timeout_seconds)} onChange={(e) => updateRuntime({default_timeout_seconds: num(e.target.value, 30)})}/></Field>
                            <Field label={T.streamingUpdateMs} help={T.directInput}><input disabled={disabled} type='number' min={100} step={100} style={field} value={String(config.runtime.streaming_update_ms)} onChange={(e) => updateRuntime({streaming_update_ms: num(e.target.value, 800)})}/></Field>
                            <Field label={T.maxInput} help={T.directInput}><input disabled={disabled} type='number' min={1} style={field} value={String(config.runtime.max_input_length)} onChange={(e) => updateRuntime({max_input_length: num(e.target.value, 4000)})}/></Field>
                        </div>
                        <div style={row2}>
                            <Field label={T.maxOutput} help={T.directInput}><input disabled={disabled} type='number' min={1} style={field} value={String(config.runtime.max_output_length)} onChange={(e) => updateRuntime({max_output_length: num(e.target.value, 8000)})}/></Field>
                            <div style={{display: 'flex', flexDirection: 'column', justifyContent: 'flex-end'}}>
                                <label><input disabled={disabled} type='checkbox' checked={config.runtime.enable_streaming} onChange={(e) => updateRuntime({enable_streaming: e.target.checked})}/>{` ${T.enableStreaming}`}</label>
                                <span style={note}>{T.enableStreamingHelp}</span>
                            </div>
                        </div>
                        <div style={row2}>
                            <Field label={T.pdfDpi}><input disabled={disabled} type='number' min={72} style={field} value={String(config.runtime.pdf_raster_dpi)} onChange={(e) => updateRuntime({pdf_raster_dpi: num(e.target.value, 200)})}/></Field>
                            <Field label={T.maxPdfPages}><input disabled={disabled} type='number' min={1} style={field} value={String(config.runtime.max_pdf_pages)} onChange={(e) => updateRuntime({max_pdf_pages: num(e.target.value, 20)})}/></Field>
                        </div>
                        <label><input disabled={disabled} type='checkbox' checked={config.runtime.mask_sensitive_data} onChange={(e) => updateRuntime({mask_sensitive_data: e.target.checked})}/>{` ${T.maskDefault}`}</label>
                        <label><input disabled={disabled} type='checkbox' checked={config.runtime.enable_debug_logs} onChange={(e) => updateRuntime({enable_debug_logs: e.target.checked})}/>{` ${T.debugLogs}`}</label>
                        <label><input disabled={disabled} type='checkbox' checked={config.runtime.enable_usage_logs} onChange={(e) => updateRuntime({enable_usage_logs: e.target.checked})}/>{` ${T.usageLogs}`}</label>
                    </>
                )}
            </section>

            <section style={card}>
                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center'}}>
                    <strong>{T.bots}</strong>
                    <div style={{display: 'flex', gap: 8}}>
                        <button className='btn btn-tertiary' disabled={disabled} type='button' onClick={loadSamples}>{T.loadSamples}</button>
                        <button className='btn btn-primary' disabled={disabled} type='button' onClick={addBot}>{T.addBot}</button>
                    </div>
                </div>
                <span style={note}>{T.addHint}</span>
                <div style={botLayout}>
                    <div style={{display: 'flex', flexDirection: 'column', gap: 8}}>
                        {config.bots.length === 0 && <div style={box}>{T.noBots}</div>}
                        {config.bots.map((item) => (
                        <button key={item.local_id} type='button' onClick={() => setSelected(selectionKey(item))} style={{...botButton, borderColor: bot?.local_id === item.local_id ? 'rgba(var(--button-bg-rgb),.45)' : 'rgba(var(--center-channel-color-rgb),.08)'}}>
                                <strong>{item.display_name || '@new-bot'}</strong>
                                <div>{`@${item.username || 'username'}`}</div>
                                <div style={note}>{`${item.model || defaultModel} | ${modeLabel(item.mode)} | ${item.output_mode} | temp=${item.temperature} | max_tokens=${item.max_tokens}`}</div>
                            </button>
                        ))}
                    </div>
                    <div style={{display: 'flex', flexDirection: 'column', gap: 12}}>
                        {!bot && <div style={box}>{T.selectBot}</div>}
                        {bot && (
                            <>
                                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12}}>
                                    <strong>{bot.display_name || '@new-bot'}</strong>
                                    <div style={{display: 'flex', gap: 8}}>
                                        <button className='btn btn-tertiary' disabled={disabled} type='button' onClick={() => duplicateBot(bot)}>{T.duplicate}</button>
                                        <button className='btn btn-danger' disabled={disabled} type='button' onClick={() => removeBot(bot.local_id)}>{T.delete}</button>
                                    </div>
                                </div>
                                <div style={row2}>
                                    <Field label={T.username} required={true} help={T.usernameHelp}><input disabled={disabled} style={field} value={bot.username} placeholder={'doc2vllm-bot'} onChange={(e) => updateUsername(bot, e.target.value)}/></Field>
                                    <Field label={T.displayName} help={T.displayNameHelp}><input disabled={disabled} style={field} value={bot.display_name} placeholder={'OCR Bot'} onChange={(e) => updateBot(bot.local_id, {display_name: e.target.value})}/></Field>
                                </div>
                                <Field label={T.description}><textarea disabled={disabled} style={{...field, minHeight: 72}} value={bot.description} onChange={(e) => updateBot(bot.local_id, {description: e.target.value})}/></Field>
                                <div style={box}><strong>{T.internalId}</strong><div style={{marginTop: 6, fontFamily: 'monospace'}}>{bot.bot_id}</div><div style={note}>{T.internalIdHelp}</div></div>
                                <div style={{...box, display: 'flex', flexDirection: 'column', gap: 6}}>
                                    <strong>{T.effectiveSettings}</strong>
                                    <div style={note}>{`${T.effectiveUrl}: ${effectiveBotBaseURL(config, bot)}`}</div>
                                    <div style={note}>{`${T.effectiveAuth}: ${effectiveBotAuthMode(config, bot)}`}</div>
                                    <div style={note}>{`${T.effectiveModel}: ${effectiveBotModel(bot)}`}</div>
                                    <div style={note}>{`${T.effectiveRefiner}: ${effectiveBotRefiner(bot)}`}</div>
                                </div>
                                <div style={row3}>
                                    <Field label={T.model} help={T.modelHelp}><input disabled={disabled} style={field} value={bot.model} placeholder={defaultModel} onChange={(e) => updateBot(bot.local_id, {model: e.target.value})}/></Field>
                                    <Field label={T.mode}>
                                        <select disabled={disabled} style={field} value={bot.mode} onChange={(e) => updateBot(bot.local_id, {mode: normalizeMode(e.target.value)})}>
                                            <option value='chat'>{T.chatMode}</option>
                                            <option value='ocr'>{T.ocrMode}</option>
                                            <option value='multimodal'>{T.multimodalMode}</option>
                                        </select>
                                    </Field>
                                    <Field label={T.outputMode}>
                                        <select disabled={disabled} style={field} value={bot.output_mode} onChange={(e) => updateBot(bot.local_id, {output_mode: text(e.target.value) || 'markdown'})}>
                                            <option value='markdown'>{'markdown'}</option>
                                            <option value='text'>{'text'}</option>
                                            <option value='json'>{'json'}</option>
                                        </select>
                                    </Field>
                                </div>
                                <Field label={T.systemPrompt}><textarea disabled={disabled} style={{...field, minHeight: 120}} value={bot.ocr_prompt} placeholder={defaultAttachmentInstruction(bot.mode)} onChange={(e) => updateBot(bot.local_id, {ocr_prompt: e.target.value})}/></Field>
                                <div style={row3}>
                                    <Field label={'temperature'} help={T.directInput}><input disabled={disabled} type='number' min={0} max={2} step='0.1' style={field} value={String(bot.temperature)} onChange={(e) => updateBot(bot.local_id, {temperature: numRange(e.target.value, 0, 0, 2)})}/></Field>
                                    <Field label={'max_tokens'} help={T.directInput}><input disabled={disabled} type='number' min={1} style={field} value={String(bot.max_tokens)} onChange={(e) => updateBot(bot.local_id, {max_tokens: num(e.target.value, 1024)})}/></Field>
                                    <Field label={'top_p'} help={T.directInput}><input disabled={disabled} type='number' min={0.1} max={1} step='0.1' style={field} value={String(bot.top_p)} onChange={(e) => updateBot(bot.local_id, {top_p: numRange(e.target.value, 1, 0.1, 1)})}/></Field>
                                </div>
                                <div style={row3}>
                                    <Field label={'repetition_penalty'} help={T.directInput}><input disabled={disabled} type='number' min={0.1} max={2} step='0.1' style={field} value={String(bot.repetition_penalty)} onChange={(e) => updateBot(bot.local_id, {repetition_penalty: numRange(e.target.value, 1, 0.1, 2)})}/></Field>
                                    <Field label={'presence_penalty'} help={T.directInput}><input disabled={disabled} type='number' min={-2} max={2} step='0.1' style={field} value={String(bot.presence_penalty)} onChange={(e) => updateBot(bot.local_id, {presence_penalty: numRange(e.target.value, 0, -2, 2)})}/></Field>
                                    <Field label={'frequency_penalty'} help={T.directInput}><input disabled={disabled} type='number' min={-2} max={2} step='0.1' style={field} value={String(bot.frequency_penalty)} onChange={(e) => updateBot(bot.local_id, {frequency_penalty: numRange(e.target.value, 0, -2, 2)})}/></Field>
                                </div>
                                <Field label={T.extraJson}><textarea disabled={disabled} style={{...field, minHeight: 96}} value={bot.extra_request_json} placeholder={'{"seed":7,"top_k":20}'} onChange={(e) => updateBot(bot.local_id, {extra_request_json: e.target.value})}/></Field>
                                <div style={row2}>
                                    <Field label={T.botBaseUrl} help={T.botBaseUrlHelp}><input disabled={disabled} style={field} value={bot.base_url} onChange={(e) => updateBot(bot.local_id, {base_url: e.target.value})}/></Field>
                                    <Field label={T.botApiKey}><input disabled={disabled} type='password' style={field} value={bot.auth_token} onChange={(e) => updateBot(bot.local_id, {auth_token: e.target.value})}/></Field>
                                </div>
                                <Field label={T.botAuthMode}>
                                    <select disabled={disabled} style={field} value={bot.auth_mode} onChange={(e) => updateBot(bot.local_id, {auth_mode: botAuth(e.target.value)})}>
                                        <option value=''>{T.useGlobal}</option>
                                        <option value='bearer'>{'Authorization: Bearer'}</option>
                                        <option value='x-api-key'>{'x-api-key'}</option>
                                    </select>
                                </Field>
                                <label><input disabled={disabled} type='checkbox' checked={bot.mask_sensitive_data} onChange={(e) => updateBot(bot.local_id, {mask_sensitive_data: e.target.checked})}/>{` ${T.maskBot}`}</label>
                                <div style={{...box, display: 'flex', flexDirection: 'column', gap: 8}}>
                                    <strong>{T.refiner}</strong>
                                    <div style={row2}>
                                        <Field label={T.refinerUrl} help={T.refinerUrlHelp}><input disabled={disabled} style={field} value={bot.vllm_base_url} onChange={(e) => updateBot(bot.local_id, {vllm_base_url: e.target.value})}/></Field>
                                        <Field label={T.refinerKey}><input disabled={disabled} type='password' style={field} value={bot.vllm_api_key} onChange={(e) => updateBot(bot.local_id, {vllm_api_key: e.target.value})}/></Field>
                                    </div>
                                    <div style={row2}>
                                        <Field label={T.refinerModel}><input disabled={disabled} style={field} value={bot.vllm_model} onChange={(e) => updateBot(bot.local_id, {vllm_model: e.target.value})}/></Field>
                                        <Field label={T.refinerScope}>
                                            <select disabled={disabled} style={field} value={bot.vllm_scope} onChange={(e) => updateBot(bot.local_id, {vllm_scope: botScope(e.target.value)})}>
                                                <option value='postprocess'>{T.initialOcr}</option>
                                                <option value='followups'>{T.followupsOnly}</option>
                                                <option value='both'>{T.both}</option>
                                            </select>
                                        </Field>
                                    </div>
                                    <Field label={T.refinerPrompt}><textarea disabled={disabled} style={{...field, minHeight: 120}} value={bot.vllm_prompt} onChange={(e) => updateBot(bot.local_id, {vllm_prompt: e.target.value})}/></Field>
                                </div>
                                <div style={row3}>
                                    <Field label={T.allowedTeams}><input disabled={disabled} style={field} value={join(bot.allowed_teams)} onChange={(e) => updateBot(bot.local_id, {allowed_teams: split(e.target.value, true)})}/></Field>
                                    <Field label={T.allowedChannels}><input disabled={disabled} style={field} value={join(bot.allowed_channels)} onChange={(e) => updateBot(bot.local_id, {allowed_channels: split(e.target.value, true)})}/></Field>
                                    <Field label={T.allowedUsers}><input disabled={disabled} style={field} value={join(bot.allowed_users)} onChange={(e) => updateBot(bot.local_id, {allowed_users: split(e.target.value, true)})}/></Field>
                                </div>
                            </>
                        )}
                    </div>
                </div>
            </section>

            <section style={card}>
                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center'}}>
                    <strong>{T.status}</strong>
                    <div style={{display: 'flex', gap: 8}}>
                        <button className='btn btn-tertiary' disabled={loadingStatus} type='button' onClick={refreshStatus}>{loadingStatus ? T.loadingStatus : T.refreshStatus}</button>
                        <button className='btn btn-primary' disabled={disabled || testing} type='button' onClick={runConnectionTest}>{testing ? T.testing : T.testConnection}</button>
                    </div>
                </div>
                <span style={note}>{T.testConnectionHelp}</span>
                {loadingStatus ? <span>{T.loadingStatus}</span> : (
                    <>
                        <div style={row3}>
                            <div style={box}><strong>{T.pluginId}</strong><div>{status?.plugin_id || manifest.id}</div></div>
                            <div style={box}><strong>{T.configuredBots}</strong><div>{String(status?.bot_count || config.bots.length)}</div></div>
                            <div style={box}><strong>{T.baseUrl}</strong><div>{status?.base_url || config.service.base_url || defaultURL}</div></div>
                        </div>
                        {status?.config_error && <div style={box}>{status.config_error}</div>}
                        {status?.bot_sync?.last_error && <div style={box}>{status.bot_sync.last_error}</div>}
                        <div style={{...box, display: 'flex', flexDirection: 'column', gap: 6}}>
                            <strong>{T.pdfSupport}</strong>
                            <div>{status?.pdf_support?.message || '-'}</div>
                            <div style={note}>{`${T.pdfTextExtractor}: ${status?.pdf_support?.text_extractor || T.unavailable}`}</div>
                            <div style={note}>{`${T.pdfRasterizer}: ${status?.pdf_support?.rasterizer || T.unavailable}`}</div>
                            {status?.pdf_support?.hint && <div style={note}>{status.pdf_support.hint}</div>}
                        </div>
                        <div style={{...box, display: 'flex', flexDirection: 'column', gap: 8}}>
                            <strong>{T.managedBots}</strong>
                            {managedBots.length === 0 && <div>{T.noManagedBots}</div>}
                            {managedBots.map((item) => <ManagedBotRow key={item.bot_id || item.username} item={item}/>)}
                        </div>
                        {connection && <div style={box}><pre style={codeStyle}>{renderConnectionStatus(connection)}</pre></div>}
                        {connectionError && <div style={box}>{connectionError}</div>}
                    </>
                )}
            </section>

            <section style={card}>
                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center'}}>
                    <strong>{T.preview}</strong>
                    <button className='btn btn-tertiary' type='button' onClick={() => setShowPreview((open) => !open)}>
                        {showPreview ? T.hidePreview : T.showPreview}
                    </button>
                </div>
                <span style={note}>{T.previewHelp}</span>
                {showPreview && <pre style={codeStyle}>{preview}</pre>}
            </section>
        </div>
    );
}

function Field(props: FieldProps) {
    return (
        <label style={{display: 'flex', flexDirection: 'column', gap: 6}}>
            <strong style={{fontSize: 13}}>{props.required ? `${props.label} *` : props.label}</strong>
            {props.children}
            {props.help && <span style={note}>{props.help}</span>}
        </label>
    );
}

function ManagedBotRow(props: {item: ManagedBotStatus}) {
    const {item} = props;
    return (
        <div style={{background: 'rgba(var(--center-channel-color-rgb),.03)', borderRadius: 8, padding: 12, border: '1px solid rgba(var(--center-channel-color-rgb),.08)'}}>
            <strong>{item.display_name || item.username}</strong>
            <div>{`@${item.username}`}</div>
            <div style={note}>{`모델=${item.model || '-'} | 등록=${item.registered ? '완료' : '미완료'} | 활성화=${item.active ? '예' : '아니오'}`}</div>
            {item.status_message && <div style={note}>{item.status_message}</div>}
        </div>
    );
}

function createDefaultConfig(): DraftConfig {
    return {
        service: {base_url: defaultURL, auth_mode: 'bearer', auth_token: '', allow_hosts: ''},
        runtime: {default_timeout_seconds: 30, enable_streaming: true, streaming_update_ms: 800, max_input_length: 4000, max_output_length: 8000, pdf_raster_dpi: 200, max_pdf_pages: 20, mask_sensitive_data: false, enable_debug_logs: false, enable_usage_logs: true},
        bots: [],
    };
}

export async function loadConfig(
    rawValue: unknown,
    last: React.MutableRefObject<string>,
    setConfig: React.Dispatch<React.SetStateAction<DraftConfig>>,
    setSource: React.Dispatch<React.SetStateAction<string>>,
    setSelected: React.Dispatch<React.SetStateAction<string>>,
    setLoadingConfig: React.Dispatch<React.SetStateAction<boolean>>,
    setError: React.Dispatch<React.SetStateAction<string>>,
) {
    const parsed = parseValue(rawValue);
    // Mattermost feeds the unsaved editor value back through props.value.
    // Prefer that local draft over the last persisted server config so
    // in-progress edits and newly added bots are not overwritten.
    if (parsed.ok) {
        setError('');
        setConfig(parsed.config);
        setSource('config');
        setSelected((current) => pickBot(parsed.config.bots, current));
        last.current = parsed.raw;
        setLoadingConfig(false);
        return;
    }

    setLoadingConfig(true);
    setError('');
    try {
        const response = await getAdminConfig();
        const next = normalizeConfig(response.config);
        setConfig(next);
        setSource(response.source || 'config');
        setSelected((current) => pickBot(next.bots, current));
        last.current = serialize(buildConfig(next));
    } catch (e) {
        setConfig(createDefaultConfig());
        setSelected('');
        setError((e as Error).message);
    } finally {
        setLoadingConfig(false);
    }
}

async function loadStatus(
    setStatus: React.Dispatch<React.SetStateAction<PluginStatus | null>>,
    setLoadingStatus: React.Dispatch<React.SetStateAction<boolean>>,
    setError: React.Dispatch<React.SetStateAction<string>>,
) {
    setLoadingStatus(true);
    try {
        setStatus(await getStatus());
    } catch (e) {
        setError((e as Error).message);
    } finally {
        setLoadingStatus(false);
    }
}

function parseValue(value: unknown): {ok: boolean; config: DraftConfig; raw: string} {
    if (value == null || value === '') {
        return {ok: false, config: createDefaultConfig(), raw: ''};
    }
    try {
        const parsed = typeof value === 'string' ? JSON.parse(value) : value;
        return {ok: true, config: normalizeConfig(parsed as AdminPluginConfig), raw: serialize(parsed)};
    } catch {
        return {ok: false, config: createDefaultConfig(), raw: ''};
    }
}

export function normalizeConfig(value?: AdminPluginConfig): DraftConfig {
    const next = createDefaultConfig();
    if (!value) {
        return next;
    }
    next.service = {
        base_url: value.service?.base_url == null ? defaultURL : text(value.service?.base_url),
        auth_mode: auth(text(value.service?.auth_mode)),
        auth_token: text(value.service?.auth_token),
        allow_hosts: value.service?.allow_hosts == null ? '' : text(value.service?.allow_hosts),
    };
    next.runtime = {default_timeout_seconds: num(value.runtime?.default_timeout_seconds, 30), enable_streaming: value.runtime?.enable_streaming !== false || !value.runtime?.streaming_update_ms, streaming_update_ms: num(value.runtime?.streaming_update_ms, 800), max_input_length: num(value.runtime?.max_input_length, 4000), max_output_length: num(value.runtime?.max_output_length, 8000), pdf_raster_dpi: num(value.runtime?.pdf_raster_dpi, 200), max_pdf_pages: num(value.runtime?.max_pdf_pages, 20), mask_sensitive_data: Boolean(value.runtime?.mask_sensitive_data), enable_debug_logs: Boolean(value.runtime?.enable_debug_logs), enable_usage_logs: value.runtime?.enable_usage_logs !== false};
    next.bots = Array.isArray(value.bots) ? value.bots.map((item, index) => normalizeBot(item, index, next.runtime.mask_sensitive_data)) : [];
    return next;
}

export function buildConfig(config: DraftConfig): AdminPluginConfig {
    return {
        service: {base_url: text(config.service.base_url), auth_mode: auth(config.service.auth_mode), auth_token: text(config.service.auth_token), allow_hosts: text(config.service.allow_hosts)},
        runtime: {default_timeout_seconds: num(config.runtime.default_timeout_seconds, 30), enable_streaming: Boolean(config.runtime.enable_streaming), streaming_update_ms: num(config.runtime.streaming_update_ms, 800), max_input_length: num(config.runtime.max_input_length, 4000), max_output_length: num(config.runtime.max_output_length, 8000), pdf_raster_dpi: num(config.runtime.pdf_raster_dpi, 200), max_pdf_pages: num(config.runtime.max_pdf_pages, 20), mask_sensitive_data: Boolean(config.runtime.mask_sensitive_data), enable_debug_logs: Boolean(config.runtime.enable_debug_logs), enable_usage_logs: Boolean(config.runtime.enable_usage_logs)},
        bots: config.bots.map((item) => ({
            id: idValue(item.bot_id || item.username, item.local_id),
            username: user(item.username),
            display_name: text(item.display_name),
            description: text(item.description),
            base_url: text(item.base_url),
            auth_mode: botAuth(item.auth_mode),
            auth_token: text(item.auth_token),
            model: text(item.model),
            mode: normalizeMode(item.mode),
            output_mode: text(item.output_mode) || 'markdown',
            ocr_prompt: text(item.ocr_prompt),
            temperature: numRange(item.temperature, 0, 0, 2),
            max_tokens: num(item.max_tokens, 1024),
            top_p: numRange(item.top_p, 1, 0.1, 1),
            repetition_penalty: numRange(item.repetition_penalty, 1, 0.1, 2),
            presence_penalty: numRange(item.presence_penalty, 0, -2, 2),
            frequency_penalty: numRange(item.frequency_penalty, 0, -2, 2),
            extra_request_json: text(item.extra_request_json),
            mask_sensitive_data: Boolean(item.mask_sensitive_data),
            vllm_base_url: text(item.vllm_base_url),
            vllm_api_key: text(item.vllm_api_key),
            vllm_model: text(item.vllm_model),
            vllm_prompt: text(item.vllm_prompt),
            vllm_scope: botScope(item.vllm_scope),
            allowed_teams: split(join(item.allowed_teams), true),
            allowed_channels: split(join(item.allowed_channels), true),
            allowed_users: split(join(item.allowed_users), true),
        })),
    };
}

function normalizeBot(item: Partial<BotDefinition>, index: number, defaultMaskSensitiveData: boolean): DraftBot {
    const local = text(item.id) || id(`bot-${index}`);
    const username = user(text(item.username));
    return {local_id: local, bot_id: idValue(text(item.id) || username, local), username, display_name: text(item.display_name), description: text(item.description), base_url: text(item.base_url), auth_mode: botAuth(text(item.auth_mode)), auth_token: text(item.auth_token), model: text(item.model), mode: normalizeMode(item.mode), output_mode: text(item.output_mode) || 'markdown', ocr_prompt: text(item.ocr_prompt), temperature: numRange(item.temperature, 0, 0, 2), max_tokens: num(item.max_tokens, 1024), top_p: numRange(item.top_p, 1, 0.1, 1), repetition_penalty: numRange(item.repetition_penalty, 1, 0.1, 2), presence_penalty: numRange(item.presence_penalty, 0, -2, 2), frequency_penalty: numRange(item.frequency_penalty, 0, -2, 2), extra_request_json: text(item.extra_request_json), mask_sensitive_data: typeof item.mask_sensitive_data === 'boolean' ? item.mask_sensitive_data : defaultMaskSensitiveData, vllm_base_url: text(item.vllm_base_url), vllm_api_key: text(item.vllm_api_key), vllm_model: text(item.vllm_model), vllm_prompt: text(item.vllm_prompt), vllm_scope: botScope(item.vllm_scope), allowed_teams: Array.isArray(item.allowed_teams) ? split(item.allowed_teams.join(','), true) : [], allowed_channels: Array.isArray(item.allowed_channels) ? split(item.allowed_channels.join(','), true) : [], allowed_users: Array.isArray(item.allowed_users) ? split(item.allowed_users.join(','), true) : []};
}

function effectiveBotBaseURL(config: DraftConfig, bot: DraftBot): string {
    return text(bot.base_url) || text(config.service.base_url) || defaultURL;
}

function effectiveBotAuthMode(config: DraftConfig, bot: DraftBot): string {
    return botAuth(bot.auth_mode) || auth(config.service.auth_mode) || 'bearer';
}

function effectiveBotModel(bot: DraftBot): string {
    return text(bot.model) || defaultModel;
}

function effectiveBotRefiner(bot: DraftBot): string {
    if (!text(bot.vllm_base_url) || !text(bot.vllm_model)) {
        return T.inactive;
    }
    return `${text(bot.vllm_model)} (${botScope(bot.vllm_scope)})`;
}

function validate(config: DraftConfig): string[] {
    const items: string[] = [];
    const usernames = new Set<string>();
    const reservedRequestKeys = new Set(['model', 'messages', 'temperature', 'max_tokens', 'top_p', 'repetition_penalty', 'presence_penalty', 'frequency_penalty']);
    if (!text(config.service.base_url)) {
        items.push('\uae30\ubcf8 URL\uc740 \ud544\uc218\uc785\ub2c8\ub2e4.');
    }
    if (config.bots.length === 0) {
        items.push('\ucd5c\uc18c 1\uac1c \uc774\uc0c1\uc758 \ubd07\uc744 \ucd94\uac00\ud574 \uc8fc\uc138\uc694.');
    }
    for (const bot of config.bots) {
        const label = bot.display_name || bot.username || bot.bot_id;
        if (!text(bot.username)) {
            items.push(`${label}: username은 필수 입력입니다.`);
        } else if (usernames.has(bot.username)) {
            items.push(`${label}: username이 중복되었습니다.`);
        } else {
            usernames.add(bot.username);
        }
        if (text(bot.vllm_base_url) && !text(bot.vllm_model)) {
            items.push(`${label}: 후처리 URL을 입력했다면 후처리 모델명도 함께 입력해 주세요.`);
        }
        if (!text(bot.vllm_base_url) && text(bot.vllm_model)) {
            items.push(`${label}: 후처리 모델명을 입력했다면 후처리 URL도 함께 입력해 주세요.`);
        }
        if (text(bot.extra_request_json)) {
            try {
                const parsed = JSON.parse(bot.extra_request_json) as unknown;
                if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                    items.push(`${label}: extra_request_json은 JSON 객체여야 합니다.`);
                } else {
                    for (const key of Object.keys(parsed as Record<string, unknown>)) {
                        if (reservedRequestKeys.has(key)) {
                            items.push(`${label}: extra_request_json에서는 ${key} 필드를 덮어쓸 수 없습니다.`);
                        }
                    }
                }
            } catch (e) {
                items.push(`${label}: JSON 오류 - ${(e as Error).message}`);
            }
        }
    }
    return items;
}

function renderConnectionStatus(status: ConnectionStatus): string {
    const lines = [status.ok ? '\uc5f0\uacb0 \uc131\uacf5' : '\uc5f0\uacb0 \uc2e4\ud328', `URL: ${status.url}`, `HTTP: ${status.status_code}`, `Message: ${status.message}`];
    if (status.bot_name || status.bot_id) {
        lines.push(`Bot: ${status.bot_name || status.bot_id}`);
    }
    if (status.model) {
        lines.push(`Model: ${status.model}`);
    }
    if (status.mode) {
        lines.push(`Mode: ${status.mode}`);
    }
    if (status.auth_mode) {
        lines.push(`Auth: ${status.auth_mode}`);
    }
    if (status.error_code) {
        lines.push(`Code: ${status.error_code}`);
    }
    if (status.detail) {
        lines.push(`Detail: ${status.detail}`);
    }
    if (status.hint) {
        lines.push(`Hint: ${status.hint}`);
    }
    return lines.join('\n');
}

function emptyBot(existingBots: DraftBot[], defaultMaskSensitiveData: boolean): DraftBot {
    const identity = nextBotIdentity(existingBots);
    const local = id('bot');
    return {local_id: local, bot_id: identity.id, username: '', display_name: '', description: '', base_url: '', auth_mode: '', auth_token: '', model: '', mode: 'chat', output_mode: 'markdown', ocr_prompt: '', temperature: 0, max_tokens: 2048, top_p: 1, repetition_penalty: 1, presence_penalty: 0, frequency_penalty: 0, extra_request_json: '', mask_sensitive_data: defaultMaskSensitiveData, vllm_base_url: '', vllm_api_key: '', vllm_model: '', vllm_prompt: '', vllm_scope: 'postprocess', allowed_teams: [], allowed_channels: [], allowed_users: []};
}

function nextBotIdentity(existingBots: DraftBot[], start = 1): {id: string; username: string; display_name: string} {
    const seen = new Set(existingBots.map((item) => item.username));
    let index = start;
    while (true) {
        const username = `doc2vllm-bot-${index}`;
        if (!seen.has(username)) {
            return {id: username, username, display_name: `Bot ${index}`};
        }
        index++;
    }
}

export function pickBot(bots: DraftBot[], current: string): string {
    if (bots.some((item) => selectionKey(item) === current)) {
        return current;
    }
    return bots[0] ? selectionKey(bots[0]) : '';
}

export function selectionKey(bot: DraftBot): string {
    return text(bot.bot_id) || text(bot.username) || bot.local_id;
}

function modeLabel(value: string): string {
    const mode = normalizeMode(value);
    if (mode === 'chat') {
        return 'chat';
    }
    return mode === 'multimodal' ? 'multimodal' : 'ocr';
}

function botScope(value: unknown): string {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'followups' || normalized === 'both') {
        return normalized;
    }
    return 'postprocess';
}

function defaultAttachmentInstruction(mode: string): string {
    const normalized = normalizeMode(mode);
    if (normalized === 'chat') {
        return '\uccab \uc694\uccad\uc774 \ud14d\uc2a4\ud2b8 \uc9c8\uc758\uc778\uc9c0, \ucca8\ubd80 \ubb38\uc11c \uc694\uc57d\uc778\uc9c0 \ud30c\uc545\ud55c \ub4a4 \ud575\uc2ec\uc744 \uac04\uacb0\ud558\uac8c \ub2f5\ud558\uc138\uc694.';
    }
    if (normalized === 'multimodal') {
        return '\ucca8\ubd80\ub41c \uc774\ubbf8\uc9c0\ub098 \ubb38\uc11c\uc758 \ubcf4\uc774\ub294 \ub0b4\uc6a9\ub9cc \uadfc\uac70\ub85c \ub2f5\ud558\uc138\uc694.';
    }
    return '\ucca8\ubd80\ub41c \ubb38\uc11c\uc758 \ud14d\uc2a4\ud2b8\ub97c \uc6d0\ubb38\uc5d0 \ucda9\uc2e4\ud558\uac8c \ucd94\ucd9c\ud558\uc138\uc694.';
}

function serialize(value: unknown): string {
    return JSON.stringify(value, null, 2);
}

function text(value: unknown): string {
    return typeof value === 'string' ? value.trim() : '';
}

function num(value: unknown, fallback: number): number {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed <= 0) {
        return fallback;
    }
    return Math.round(parsed);
}

function numRange(value: unknown, fallback: number, min: number, max: number): number {
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) {
        return fallback;
    }
    if (parsed < min) {
        return min;
    }
    if (parsed > max) {
        return max;
    }
    return Math.round(parsed * 1000) / 1000;
}

function auth(value: unknown): string {
    return String(value || '').trim().toLowerCase() === 'x-api-key' ? 'x-api-key' : 'bearer';
}

function botAuth(value: unknown): string {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'bearer' || normalized === 'x-api-key') {
        return normalized;
    }
    return '';
}

function normalizeMode(value: unknown): string {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'chat' || normalized === 'text' || normalized === 'text-generation' || normalized === 'generation') {
        return 'chat';
    }
    if (normalized === 'multimodal' || normalized === 'vision' || normalized === 'vlm') {
        return 'multimodal';
    }
    return 'ocr';
}

function user(value: unknown): string {
    return String(value || '').trim().toLowerCase().replace(/^@+/, '').replace(/[^a-z0-9._-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
}

export function draftUsername(value: unknown): string {
    return String(value || '').toLowerCase().replace(/^@+/, '').replace(/[^a-z0-9._-]/g, '-').replace(/-+/g, '-');
}

function idValue(value: unknown, fallback: string): string {
    const normalized = String(value || '').trim().toLowerCase().replace(/[^a-z0-9._-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
    return normalized || fallback;
}

function id(prefix: string): string {
    return `${prefix}-${Math.random().toString(36).slice(2, 10)}`;
}

function join(values: string[]): string {
    return values.join(', ');
}

function split(value: string, lowerCase: boolean): string[] {
    const seen = new Set<string>();
    const items: string[] = [];
    for (const raw of value.split(/[\r\n,]+/)) {
        const next = lowerCase ? raw.trim().toLowerCase() : raw.trim();
        if (!next || seen.has(next)) {
            continue;
        }
        seen.add(next);
        items.push(next);
    }
    return items;
}

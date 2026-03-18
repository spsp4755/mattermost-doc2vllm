import manifest from 'manifest';
import React, {useEffect, useMemo, useRef, useState} from 'react';

import type {AdminPluginConfig, BotDefinition, ConnectionStatus, PluginStatus} from '../client';
import {getAdminConfig, getStatus, testConnection} from '../client';

const defaultURL = 'http://localhost:8000/v1/chat/completions';
const defaultModel = 'doc2vllm-ocr';
const defaultMultimodalModel = 'Qwen/Qwen2.5-VL-7B-Instruct';

const stack: React.CSSProperties = {display: 'flex', flexDirection: 'column', gap: 16};
const card: React.CSSProperties = {background: 'white', border: '1px solid rgba(63,67,80,.12)', borderRadius: 8, padding: 20, display: 'flex', flexDirection: 'column', gap: 12};
const row2: React.CSSProperties = {display: 'grid', gridTemplateColumns: 'repeat(2,minmax(0,1fr))', gap: 12};
const row3: React.CSSProperties = {display: 'grid', gridTemplateColumns: 'repeat(3,minmax(0,1fr))', gap: 12};
const botLayout: React.CSSProperties = {display: 'grid', gridTemplateColumns: '280px minmax(0,1fr)', gap: 16};
const field: React.CSSProperties = {width: '100%', border: '1px solid rgba(63,67,80,.16)', borderRadius: 8, padding: '10px 12px'};
const note: React.CSSProperties = {fontSize: 12, opacity: 0.75};
const box: React.CSSProperties = {padding: 12, borderRadius: 8, background: 'rgba(var(--button-bg-rgb),.08)', border: '1px solid rgba(var(--button-bg-rgb),.18)'};
const botButton: React.CSSProperties = {padding: 12, borderRadius: 8, background: 'rgba(var(--center-channel-color-rgb),.03)', border: '1px solid rgba(var(--center-channel-color-rgb),.08)', textAlign: 'left', display: 'flex', flexDirection: 'column', gap: 4};
const codeStyle: React.CSSProperties = {margin: 0, fontSize: 12, lineHeight: 1.5, background: 'rgba(var(--center-channel-color-rgb),.04)', borderRadius: 8, padding: 12, overflowX: 'auto'};

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
    service: {
        base_url: string;
        auth_mode: string;
        auth_token: string;
        allow_hosts: string;
    };
    runtime: {
        default_timeout_seconds: number;
        max_input_length: number;
        max_output_length: number;
        pdf_raster_dpi: number;
        max_pdf_pages: number;
        mask_sensitive_data: boolean;
        enable_debug_logs: boolean;
        enable_usage_logs: boolean;
    };
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

type FieldProps = {
    label: string;
    help?: string;
    children: React.ReactNode;
};

const sampleBots: Partial<BotDefinition>[] = [
    {username: 'doc2vllm-ocr', display_name: 'OCR Basic', description: 'Generic OCR bot for text extraction', model: defaultModel, mode: 'ocr', output_mode: 'markdown', ocr_prompt: 'Extract the visible text faithfully. Do not invent, correct, or summarize missing content.', temperature: 0, max_tokens: 1024, top_p: 1, repetition_penalty: 1, mask_sensitive_data: false},
    {username: 'doc2vllm-table', display_name: 'OCR Table', description: 'OCR bot tuned for tables and forms', model: defaultModel, mode: 'ocr', output_mode: 'markdown', ocr_prompt: 'Extract the document faithfully. Keep table structure only when it is clearly visible. Do not invent cells or values.', temperature: 0, max_tokens: 2048, top_p: 1, repetition_penalty: 1, mask_sensitive_data: false},
    {username: 'doc2vllm-qwen-vl', display_name: 'Qwen Multimodal', description: 'Example multimodal bot for Qwen or any OpenAI-compatible VLM', model: defaultMultimodalModel, mode: 'multimodal', output_mode: 'markdown', ocr_prompt: 'Analyze the attached image or document and answer using only the visible contents.', temperature: 0, max_tokens: 2048, top_p: 1, repetition_penalty: 1, extra_request_json: '{"min_pixels":3136,"max_pixels":12845056}', mask_sensitive_data: false},
];

export default function ConfigSetting(props: Props) {
    const key = props.id || 'Config';
    const disabled = Boolean(props.disabled || props.setByEnv);
    const [config, setConfig] = useState<DraftConfig>(createDefaultConfig());
    const [selected, setSelected] = useState('');
    const [status, setStatus] = useState<PluginStatus | null>(null);
    const [connection, setConnection] = useState<ConnectionStatus | null>(null);
    const [connectionError, setConnectionError] = useState('');
    const [source, setSource] = useState('config');
    const [error, setError] = useState('');
    const [loadingConfig, setLoadingConfig] = useState(true);
    const [loadingStatus, setLoadingStatus] = useState(true);
    const [testing, setTesting] = useState(false);
    const last = useRef('');

    useEffect(() => {
        void loadConfig(props.value, last, setConfig, setSource, setSelected, setLoadingConfig, setError);
    }, [props.value]);

    useEffect(() => {
        void loadStatus(setStatus, setLoadingStatus, setError);
    }, []);

    const bot = useMemo(() => config.bots.find((item) => item.local_id === selected) || config.bots[0] || null, [config.bots, selected]);
    const messages = useMemo(() => validate(config), [config]);
    const preview = useMemo(() => serialize(buildConfig(config)), [config]);

    const apply = (next: DraftConfig, nextSelected?: string) => {
        setConfig(next);
        const raw = serialize(buildConfig(next));
        last.current = raw;
        props.onChange(key, raw);
        props.setSaveNeeded?.();
        setSelected(nextSelected || pickBot(next.bots, selected));
    };

    const updateService = (patch: Partial<DraftConfig['service']>) => {
        apply({...config, service: {...config.service, ...patch}});
    };

    const updateRuntime = (patch: Partial<DraftConfig['runtime']>) => {
        apply({...config, runtime: {...config.runtime, ...patch}});
    };

    const updateBot = (idValue: string, patch: Partial<DraftBot>) => {
        apply({
            ...config,
            bots: config.bots.map((item) => item.local_id === idValue ? {...item, ...patch} : item),
        }, idValue);
    };

    const addBot = () => {
        const next = emptyBot(config.runtime.mask_sensitive_data);
        apply({...config, bots: [...config.bots, next]}, next.local_id);
    };

    const loadSamples = () => {
        const bots = sampleBots.map((item, index) => normalizeBot(item, index, config.runtime.mask_sensitive_data));
        apply({...config, bots}, bots[0]?.local_id);
    };

    const duplicateBot = (current: DraftBot) => {
        const next: DraftBot = {
            ...current,
            local_id: id('bot'),
            bot_id: id('bot'),
            username: current.username ? `${current.username}-copy` : '',
            display_name: current.display_name ? `${current.display_name} Copy` : '',
            allowed_teams: [...current.allowed_teams],
            allowed_channels: [...current.allowed_channels],
            allowed_users: [...current.allowed_users],
        };
        apply({...config, bots: [...config.bots, next]}, next.local_id);
    };

    const removeBot = (idValue: string) => {
        apply({...config, bots: config.bots.filter((item) => item.local_id !== idValue)});
    };

    const runConnectionTest = async () => {
        setTesting(true);
        setConnection(null);
        setConnectionError('');
        try {
            setConnection(await testConnection());
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
                    <strong>{'Doc2VLLM Plugin Settings'}</strong>
                    <span style={{fontSize: 12, fontWeight: 700}}>{manifest.version}</span>
                </div>
                <span style={note}>
                    {'Configure generic OCR bots, multimodal bots, and optional large-model refiners. Any OpenAI-compatible chat-completions endpoint can be used.'}
                </span>
                <div style={box}>
                    <div>{'OCR mode is extraction-first and works well for OCR-specialized models.'}</div>
                    <div>{'Multimodal mode is for vision-language models such as Qwen VL and other OpenAI-compatible multimodal endpoints.'}</div>
                    <div>{'Use extra_request_json for provider-specific fields such as min_pixels, max_pixels, top_k, seed, or vendor extensions.'}</div>
                </div>
                {source !== 'config' && <div style={box}>{`Loaded source: ${source}`}</div>}
                {props.setByEnv && <div style={box}>{'This setting is managed by environment variables and is read-only here.'}</div>}
                {props.helpText}
                {error && <div style={box}>{error}</div>}
                {messages.length > 0 && <div style={box}>{messages.map((message) => <div key={message}>{message}</div>)}</div>}
            </section>

            <section style={card}>
                <strong>{'Service'}</strong>
                {loadingConfig ? <span>{'Loading configuration...'}</span> : (
                    <>
                        <div style={row2}>
                            <Field label={'Base URL'}>
                                <input
                                    disabled={disabled}
                                    style={field}
                                    value={config.service.base_url}
                                    placeholder={defaultURL}
                                    onChange={(e) => updateService({base_url: e.target.value})}
                                />
                            </Field>
                            <Field label={'Auth Mode'}>
                                <select
                                    disabled={disabled}
                                    style={field}
                                    value={config.service.auth_mode}
                                    onChange={(e) => updateService({auth_mode: auth(e.target.value)})}
                                >
                                    <option value='bearer'>{'Authorization: Bearer'}</option>
                                    <option value='x-api-key'>{'x-api-key'}</option>
                                </select>
                            </Field>
                        </div>
                        <div style={row2}>
                            <Field label={'API Key'}>
                                <input
                                    disabled={disabled}
                                    type='password'
                                    style={field}
                                    value={config.service.auth_token}
                                    onChange={(e) => updateService({auth_token: e.target.value})}
                                />
                            </Field>
                            <Field label={'Allowed Hosts'} help={'Comma-separated hostnames or wildcard hosts'}>
                                <input
                                    disabled={disabled}
                                    style={field}
                                    value={config.service.allow_hosts}
                                    placeholder={'localhost, *.internal.example.com'}
                                    onChange={(e) => updateService({allow_hosts: e.target.value})}
                                />
                            </Field>
                        </div>
                        <div style={row3}>
                            <Field label={'Timeout (s)'}>
                                <input
                                    disabled={disabled}
                                    type='number'
                                    min={1}
                                    style={field}
                                    value={String(config.runtime.default_timeout_seconds)}
                                    onChange={(e) => updateRuntime({default_timeout_seconds: num(e.target.value, 30)})}
                                />
                            </Field>
                            <Field label={'Max Input'}>
                                <input
                                    disabled={disabled}
                                    type='number'
                                    min={1}
                                    style={field}
                                    value={String(config.runtime.max_input_length)}
                                    onChange={(e) => updateRuntime({max_input_length: num(e.target.value, 4000)})}
                                />
                            </Field>
                            <Field label={'Max Output'}>
                                <input
                                    disabled={disabled}
                                    type='number'
                                    min={1}
                                    style={field}
                                    value={String(config.runtime.max_output_length)}
                                    onChange={(e) => updateRuntime({max_output_length: num(e.target.value, 8000)})}
                                />
                            </Field>
                        </div>
                        <div style={row2}>
                            <Field label={'PDF DPI'}>
                                <input
                                    disabled={disabled}
                                    type='number'
                                    min={72}
                                    style={field}
                                    value={String(config.runtime.pdf_raster_dpi)}
                                    onChange={(e) => updateRuntime({pdf_raster_dpi: num(e.target.value, 200)})}
                                />
                            </Field>
                            <Field label={'Max PDF Pages'}>
                                <input
                                    disabled={disabled}
                                    type='number'
                                    min={1}
                                    style={field}
                                    value={String(config.runtime.max_pdf_pages)}
                                    onChange={(e) => updateRuntime({max_pdf_pages: num(e.target.value, 20)})}
                                />
                            </Field>
                        </div>
                        <label><input disabled={disabled} type='checkbox' checked={config.runtime.mask_sensitive_data} onChange={(e) => updateRuntime({mask_sensitive_data: e.target.checked})}/>{' Mask sensitive data by default'}</label>
                        <label><input disabled={disabled} type='checkbox' checked={config.runtime.enable_debug_logs} onChange={(e) => updateRuntime({enable_debug_logs: e.target.checked})}/>{' Enable debug logs'}</label>
                        <label><input disabled={disabled} type='checkbox' checked={config.runtime.enable_usage_logs} onChange={(e) => updateRuntime({enable_usage_logs: e.target.checked})}/>{' Enable usage logs'}</label>
                    </>
                )}
            </section>

            <section style={card}>
                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center'}}>
                    <strong>{'Bots'}</strong>
                    <div style={{display: 'flex', gap: 8}}>
                        <button className='btn btn-tertiary' disabled={disabled} type='button' onClick={loadSamples}>
                            {'Load Samples'}
                        </button>
                        <button className='btn btn-primary' disabled={disabled} type='button' onClick={addBot}>
                            {'Add Bot'}
                        </button>
                    </div>
                </div>
                <div style={botLayout}>
                    <div style={{display: 'flex', flexDirection: 'column', gap: 8}}>
                        {config.bots.length === 0 && <div style={box}>{'No bots configured yet.'}</div>}
                        {config.bots.map((item) => (
                            <button
                                key={item.local_id}
                                type='button'
                                onClick={() => setSelected(item.local_id)}
                                style={{
                                    ...botButton,
                                    borderColor: bot?.local_id === item.local_id ? 'rgba(var(--button-bg-rgb),.45)' : 'rgba(var(--center-channel-color-rgb),.08)',
                                }}
                            >
                                <strong>{item.display_name || '@new-bot'}</strong>
                                <div>{`@${item.username || 'username'}`}</div>
                                <div style={note}>
                                    {`${item.model || defaultModel} | ${normalizeMode(item.mode)} | ${item.output_mode} | temp=${item.temperature} | max_tokens=${item.max_tokens}`}
                                </div>
                                {item.vllm_model && <div style={note}>{`Refiner: ${item.vllm_model} (${botScope(item.vllm_scope)})`}</div>}
                            </button>
                        ))}
                    </div>
                    <div style={{display: 'flex', flexDirection: 'column', gap: 12}}>
                        {!bot && <div style={box}>{'Select a bot from the list or create a new one.'}</div>}
                        {bot && (
                            <>
                                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12}}>
                                    <strong>{bot.display_name || '@new-bot'}</strong>
                                    <div style={{display: 'flex', gap: 8}}>
                                        <button className='btn btn-tertiary' disabled={disabled} type='button' onClick={() => duplicateBot(bot)}>
                                            {'Duplicate'}
                                        </button>
                                        <button className='btn btn-danger' disabled={disabled} type='button' onClick={() => removeBot(bot.local_id)}>
                                            {'Delete'}
                                        </button>
                                    </div>
                                </div>

                                <div style={row2}>
                                    <Field label={'Username'} help={'Mattermost bot handle without @'}>
                                        <input disabled={disabled} style={field} value={bot.username} placeholder={'doc2vllm-ocr'} onChange={(e) => updateBot(bot.local_id, {username: user(e.target.value)})}/>
                                    </Field>
                                    <Field label={'Display Name'}>
                                        <input disabled={disabled} style={field} value={bot.display_name} onChange={(e) => updateBot(bot.local_id, {display_name: e.target.value})}/>
                                    </Field>
                                </div>

                                <Field label={'Bot ID'} help={'Stable internal identifier used by the plugin'}>
                                    <input disabled={disabled} style={field} value={bot.bot_id} onChange={(e) => updateBot(bot.local_id, {bot_id: idValue(e.target.value, bot.local_id)})}/>
                                </Field>

                                <Field label={'Description'}>
                                    <textarea disabled={disabled} style={{...field, minHeight: 72}} value={bot.description} onChange={(e) => updateBot(bot.local_id, {description: e.target.value})}/>
                                </Field>

                                <div style={row3}>
                                    <Field label={'Model'} help={'Any OCR or multimodal model name accepted by your endpoint'}>
                                        <input disabled={disabled} style={field} value={bot.model} placeholder={defaultModel} onChange={(e) => updateBot(bot.local_id, {model: e.target.value})}/>
                                    </Field>
                                    <Field label={'Mode'}>
                                        <select disabled={disabled} style={field} value={bot.mode} onChange={(e) => updateBot(bot.local_id, {mode: normalizeMode(e.target.value)})}>
                                            <option value='ocr'>{'OCR / extraction'}</option>
                                            <option value='multimodal'>{'Multimodal / vision-language'}</option>
                                        </select>
                                    </Field>
                                    <Field label={'Output Mode'}>
                                        <select disabled={disabled} style={field} value={bot.output_mode} onChange={(e) => updateBot(bot.local_id, {output_mode: text(e.target.value) || 'markdown'})}>
                                            <option value='markdown'>{'markdown'}</option>
                                            <option value='text'>{'text'}</option>
                                            <option value='json'>{'json'}</option>
                                        </select>
                                    </Field>
                                </div>

                                <Field label={'Attachment System Prompt'} help={'Used only when the request includes an attachment'}>
                                    <textarea disabled={disabled} style={{...field, minHeight: 120}} value={bot.ocr_prompt} placeholder={defaultAttachmentInstruction(bot.mode)} onChange={(e) => updateBot(bot.local_id, {ocr_prompt: e.target.value})}/>
                                </Field>

                                <div style={row3}>
                                    <Field label={'Temperature'}><input disabled={disabled} type='number' min={0} max={2} step='0.1' style={field} value={String(bot.temperature)} onChange={(e) => updateBot(bot.local_id, {temperature: numRange(e.target.value, 0, 0, 2)})}/></Field>
                                    <Field label={'Max Tokens'}><input disabled={disabled} type='number' min={1} style={field} value={String(bot.max_tokens)} onChange={(e) => updateBot(bot.local_id, {max_tokens: num(e.target.value, 1024)})}/></Field>
                                    <Field label={'Top P'}><input disabled={disabled} type='number' min={0.1} max={1} step='0.1' style={field} value={String(bot.top_p)} onChange={(e) => updateBot(bot.local_id, {top_p: numRange(e.target.value, 1, 0.1, 1)})}/></Field>
                                </div>

                                <div style={row3}>
                                    <Field label={'Repetition Penalty'}><input disabled={disabled} type='number' min={0.1} max={2} step='0.1' style={field} value={String(bot.repetition_penalty)} onChange={(e) => updateBot(bot.local_id, {repetition_penalty: numRange(e.target.value, 1, 0.1, 2)})}/></Field>
                                    <Field label={'Presence Penalty'}><input disabled={disabled} type='number' min={-2} max={2} step='0.1' style={field} value={String(bot.presence_penalty)} onChange={(e) => updateBot(bot.local_id, {presence_penalty: numRange(e.target.value, 0, -2, 2)})}/></Field>
                                    <Field label={'Frequency Penalty'}><input disabled={disabled} type='number' min={-2} max={2} step='0.1' style={field} value={String(bot.frequency_penalty)} onChange={(e) => updateBot(bot.local_id, {frequency_penalty: numRange(e.target.value, 0, -2, 2)})}/></Field>
                                </div>

                                <Field label={'extra_request_json'} help={'Pass provider-specific parameters as raw JSON'}>
                                    <textarea disabled={disabled} style={{...field, minHeight: 96}} value={bot.extra_request_json} placeholder={'{"min_pixels":3136,"max_pixels":12845056,"seed":7}'} onChange={(e) => updateBot(bot.local_id, {extra_request_json: e.target.value})}/>
                                </Field>
                                <span style={note}>{'Examples: Qwen multimodal pixel limits, vendor-specific sampling parameters, or any extra OpenAI-compatible request body fields not already exposed above.'}</span>

                                <div style={row2}>
                                    <Field label={'Bot-specific Base URL'} help={'Leave empty to use the global service URL'}>
                                        <input disabled={disabled} style={field} value={bot.base_url} placeholder={defaultURL} onChange={(e) => updateBot(bot.local_id, {base_url: e.target.value})}/>
                                    </Field>
                                    <Field label={'Bot-specific API Key'} help={'Leave empty to use the global service key'}>
                                        <input disabled={disabled} type='password' style={field} value={bot.auth_token} onChange={(e) => updateBot(bot.local_id, {auth_token: e.target.value})}/>
                                    </Field>
                                </div>

                                <Field label={'Bot-specific Auth Mode'}>
                                    <select disabled={disabled} style={field} value={bot.auth_mode} onChange={(e) => updateBot(bot.local_id, {auth_mode: botAuth(e.target.value)})}>
                                        <option value=''>{'Use global setting'}</option>
                                        <option value='bearer'>{'Authorization: Bearer'}</option>
                                        <option value='x-api-key'>{'x-api-key'}</option>
                                    </select>
                                </Field>

                                <label><input disabled={disabled} type='checkbox' checked={bot.mask_sensitive_data} onChange={(e) => updateBot(bot.local_id, {mask_sensitive_data: e.target.checked})}/>{' Mask sensitive data for this bot'}</label>
                                <div style={{...box, display: 'flex', flexDirection: 'column', gap: 8}}>
                                    <strong>{'Large-model Refiner (optional)'}</strong>
                                    <span style={note}>{'Use this with a larger model such as MiniMax or another LLM/VLM to rewrite OCR output or answer follow-up questions from the extracted document context.'}</span>
                                    <div style={row2}>
                                        <Field label={'Refiner URL'}>
                                            <input disabled={disabled} style={field} value={bot.vllm_base_url} placeholder={'http://localhost:8001/v1'} onChange={(e) => updateBot(bot.local_id, {vllm_base_url: e.target.value})}/>
                                        </Field>
                                        <Field label={'Refiner API Key'}>
                                            <input disabled={disabled} type='password' style={field} value={bot.vllm_api_key} onChange={(e) => updateBot(bot.local_id, {vllm_api_key: e.target.value})}/>
                                        </Field>
                                    </div>
                                    <div style={row2}>
                                        <Field label={'Refiner Model'}>
                                            <input disabled={disabled} style={field} value={bot.vllm_model} placeholder={'MiniMax-M2.5'} onChange={(e) => updateBot(bot.local_id, {vllm_model: e.target.value})}/>
                                        </Field>
                                        <Field label={'Refiner Scope'}>
                                            <select disabled={disabled} style={field} value={bot.vllm_scope} onChange={(e) => updateBot(bot.local_id, {vllm_scope: botScope(e.target.value)})}>
                                                <option value='postprocess'>{'Initial OCR only'}</option>
                                                <option value='followups'>{'Follow-up chat only'}</option>
                                                <option value='both'>{'Initial OCR + follow-ups'}</option>
                                            </select>
                                        </Field>
                                    </div>
                                    <Field label={'Refiner Prompt'} help={'Optional template. {{user_message}} and {{document_text}} are supported.'}>
                                        <textarea disabled={disabled} style={{...field, minHeight: 120}} value={bot.vllm_prompt} onChange={(e) => updateBot(bot.local_id, {vllm_prompt: e.target.value})}/>
                                    </Field>
                                </div>

                                <div style={row3}>
                                    <Field label={'Allowed Teams'} help={'Comma-separated team names or IDs'}>
                                        <input disabled={disabled} style={field} value={join(bot.allowed_teams)} placeholder={'engineering, finance'} onChange={(e) => updateBot(bot.local_id, {allowed_teams: split(e.target.value, true)})}/>
                                    </Field>
                                    <Field label={'Allowed Channels'} help={'Comma-separated channel names or IDs'}>
                                        <input disabled={disabled} style={field} value={join(bot.allowed_channels)} placeholder={'town-square, contracts'} onChange={(e) => updateBot(bot.local_id, {allowed_channels: split(e.target.value, true)})}/>
                                    </Field>
                                    <Field label={'Allowed Users'} help={'Comma-separated usernames or user IDs'}>
                                        <input disabled={disabled} style={field} value={join(bot.allowed_users)} placeholder={'alice, bob'} onChange={(e) => updateBot(bot.local_id, {allowed_users: split(e.target.value, true)})}/>
                                    </Field>
                                </div>
                            </>
                        )}
                    </div>
                </div>
            </section>

            <section style={card}>
                <div style={{display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center'}}>
                    <strong>{'Connection & Status'}</strong>
                    <button className='btn btn-primary' disabled={disabled || testing} type='button' onClick={runConnectionTest}>
                        {testing ? 'Testing...' : 'Test Connection'}
                    </button>
                </div>
                {loadingStatus ? <span>{'Loading status...'}</span> : (
                    <>
                        <div style={row3}>
                            <div style={box}><strong>{'Plugin ID'}</strong><div>{status?.plugin_id || manifest.id}</div></div>
                            <div style={box}><strong>{'Configured Bots'}</strong><div>{String(status?.bot_count || config.bots.length)}</div></div>
                            <div style={box}><strong>{'Base URL'}</strong><div>{status?.base_url || config.service.base_url || defaultURL}</div></div>
                        </div>
                        {status?.config_error && <div style={box}>{status.config_error}</div>}
                        {connection && <div style={box}>{curl(connection)}</div>}
                        {connectionError && <div style={box}>{connectionError}</div>}
                    </>
                )}
            </section>

            <section style={card}>
                <strong>{'Generated Config JSON'}</strong>
                <span style={note}>{'This is the JSON stored in the Mattermost plugin setting.'}</span>
                <pre style={codeStyle}>{preview}</pre>
            </section>
        </div>
    );
}

function Field(props: FieldProps) {
    return (
        <label style={{display: 'flex', flexDirection: 'column', gap: 6}}>
            <strong style={{fontSize: 13}}>{props.label}</strong>
            {props.children}
            {props.help && <span style={note}>{props.help}</span>}
        </label>
    );
}

function createDefaultConfig(): DraftConfig {
    return {
        service: {
            base_url: defaultURL,
            auth_mode: 'bearer',
            auth_token: '',
            allow_hosts: 'localhost',
        },
        runtime: {
            default_timeout_seconds: 30,
            max_input_length: 4000,
            max_output_length: 8000,
            pdf_raster_dpi: 200,
            max_pdf_pages: 20,
            mask_sensitive_data: false,
            enable_debug_logs: false,
            enable_usage_logs: true,
        },
        bots: [],
    };
}

async function loadConfig(
    rawValue: unknown,
    last: React.MutableRefObject<string>,
    setConfig: React.Dispatch<React.SetStateAction<DraftConfig>>,
    setSource: React.Dispatch<React.SetStateAction<string>>,
    setSelected: React.Dispatch<React.SetStateAction<string>>,
    setLoadingConfig: React.Dispatch<React.SetStateAction<boolean>>,
    setError: React.Dispatch<React.SetStateAction<string>>,
) {
    setLoadingConfig(true);
    setError('');
    try {
        if (typeof rawValue === 'string' && rawValue.trim() !== '' && rawValue !== last.current) {
            const next = normalizeConfig(parseValue(rawValue));
            setConfig(next);
            setSource('config');
            setSelected(next.bots[0]?.local_id || '');
            last.current = rawValue;
            return;
        }

        const response = await getAdminConfig();
        const next = normalizeConfig(response.config);
        setConfig(next);
        setSource(response.source || 'config');
        setSelected(next.bots[0]?.local_id || '');
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

function parseValue(value: unknown): unknown {
    if (typeof value === 'string') {
        return JSON.parse(value);
    }
    return value;
}

function normalizeConfig(value: unknown): DraftConfig {
    const typed = (value || {}) as Partial<AdminPluginConfig>;
    const service = (typed.service || {}) as Partial<AdminPluginConfig['service']>;
    const runtime = (typed.runtime || {}) as Partial<AdminPluginConfig['runtime']>;
    const defaultMask = Boolean(runtime.mask_sensitive_data);

    return {
        service: {
            base_url: text(service.base_url) || defaultURL,
            auth_mode: auth(service.auth_mode),
            auth_token: text(service.auth_token),
            allow_hosts: text(service.allow_hosts) || 'localhost',
        },
        runtime: {
            default_timeout_seconds: num(runtime.default_timeout_seconds, 30),
            max_input_length: num(runtime.max_input_length, 4000),
            max_output_length: num(runtime.max_output_length, 8000),
            pdf_raster_dpi: num(runtime.pdf_raster_dpi, 200),
            max_pdf_pages: num(runtime.max_pdf_pages, 20),
            mask_sensitive_data: defaultMask,
            enable_debug_logs: Boolean(runtime.enable_debug_logs),
            enable_usage_logs: runtime.enable_usage_logs !== false,
        },
        bots: Array.isArray(typed.bots) ? typed.bots.map((item, index) => normalizeBot(item, index, defaultMask)) : [],
    };
}

function buildConfig(config: DraftConfig): AdminPluginConfig {
    return {
        service: {
            base_url: text(config.service.base_url) || defaultURL,
            auth_mode: auth(config.service.auth_mode),
            auth_token: text(config.service.auth_token),
            allow_hosts: text(config.service.allow_hosts),
        },
        runtime: {
            default_timeout_seconds: num(config.runtime.default_timeout_seconds, 30),
            max_input_length: num(config.runtime.max_input_length, 4000),
            max_output_length: num(config.runtime.max_output_length, 8000),
            pdf_raster_dpi: num(config.runtime.pdf_raster_dpi, 200),
            max_pdf_pages: num(config.runtime.max_pdf_pages, 20),
            mask_sensitive_data: Boolean(config.runtime.mask_sensitive_data),
            enable_debug_logs: Boolean(config.runtime.enable_debug_logs),
            enable_usage_logs: Boolean(config.runtime.enable_usage_logs),
        },
        bots: config.bots.map((item) => ({
            id: text(item.bot_id) || item.local_id,
            username: user(item.username),
            display_name: text(item.display_name),
            description: text(item.description),
            base_url: text(item.base_url),
            auth_mode: botAuth(item.auth_mode),
            auth_token: text(item.auth_token),
            model: text(item.model) || defaultModel,
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
    return {
        local_id: local,
        bot_id: text(item.id) || local,
        username: text(item.username),
        display_name: text(item.display_name),
        description: text(item.description),
        base_url: text(item.base_url),
        auth_mode: botAuth(item.auth_mode),
        auth_token: text(item.auth_token),
        model: text(item.model) || defaultModel,
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
        mask_sensitive_data: typeof item.mask_sensitive_data === 'boolean' ? item.mask_sensitive_data : defaultMaskSensitiveData,
        vllm_base_url: text(item.vllm_base_url),
        vllm_api_key: text(item.vllm_api_key),
        vllm_model: text(item.vllm_model),
        vllm_prompt: text(item.vllm_prompt),
        vllm_scope: botScope(item.vllm_scope),
        allowed_teams: Array.isArray(item.allowed_teams) ? split(item.allowed_teams.join(','), true) : [],
        allowed_channels: Array.isArray(item.allowed_channels) ? split(item.allowed_channels.join(','), true) : [],
        allowed_users: Array.isArray(item.allowed_users) ? split(item.allowed_users.join(','), true) : [],
    };
}

function validate(config: DraftConfig): string[] {
    const messages: string[] = [];
    if (!text(config.service.base_url)) {
        messages.push('Base URL is required.');
    }
    if (config.bots.length === 0) {
        messages.push('Add at least one bot before saving.');
    }

    const seen = new Set<string>();
    for (const bot of config.bots) {
        if (!text(bot.username)) {
            messages.push(`Bot "${bot.display_name || bot.bot_id}" is missing a username.`);
        }
        const username = user(bot.username);
        if (username && seen.has(username)) {
            messages.push(`Duplicate bot username: ${username}`);
        }
        if (username) {
            seen.add(username);
        }
        if (text(bot.extra_request_json)) {
            try {
                const parsed = JSON.parse(bot.extra_request_json) as unknown;
                if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                    messages.push(`extra_request_json for @${username || bot.bot_id} must be a JSON object.`);
                }
            } catch (e) {
                messages.push(`extra_request_json for @${username || bot.bot_id} is invalid JSON: ${(e as Error).message}`);
            }
        }
        const hasRefinerURL = text(bot.vllm_base_url) !== '';
        const hasRefinerModel = text(bot.vllm_model) !== '';
        if (hasRefinerURL !== hasRefinerModel) {
            messages.push(`Refiner URL and Refiner Model must both be set for @${username || bot.bot_id}.`);
        }
    }

    return messages;
}

function curl(status: ConnectionStatus): string {
    const lines = [
        status.ok ? 'Connection succeeded.' : 'Connection failed.',
        `URL: ${status.url}`,
        `HTTP status: ${status.status_code}`,
        `Message: ${status.message}`,
    ];
    if (status.error_code) {
        lines.push(`Error code: ${status.error_code}`);
    }
    if (status.detail) {
        lines.push(`Detail: ${status.detail}`);
    }
    if (status.hint) {
        lines.push(`Hint: ${status.hint}`);
    }
    return lines.join('\n');
}

function emptyBot(defaultMaskSensitiveData: boolean): DraftBot {
    const local = id('bot');
    return {
        local_id: local,
        bot_id: local,
        username: '',
        display_name: '',
        description: '',
        base_url: '',
        auth_mode: '',
        auth_token: '',
        model: defaultModel,
        mode: 'ocr',
        output_mode: 'markdown',
        ocr_prompt: '',
        temperature: 0,
        max_tokens: 1024,
        top_p: 1,
        repetition_penalty: 1,
        presence_penalty: 0,
        frequency_penalty: 0,
        extra_request_json: '',
        mask_sensitive_data: defaultMaskSensitiveData,
        vllm_base_url: '',
        vllm_api_key: '',
        vllm_model: '',
        vllm_prompt: '',
        vllm_scope: 'postprocess',
        allowed_teams: [],
        allowed_channels: [],
        allowed_users: [],
    };
}

function pickBot(bots: DraftBot[], current: string): string {
    if (bots.some((item) => item.local_id === current)) {
        return current;
    }
    return bots[0]?.local_id || '';
}

function serialize(value: unknown): string {
    return JSON.stringify(value, null, 2);
}

function text(value: unknown): string {
    return typeof value === 'string' ? value.trim() : '';
}

function defaultAttachmentInstruction(mode: string): string {
    if (normalizeMode(mode) === 'multimodal') {
        return 'Analyze the attached image or document and answer using only its visible contents.';
    }
    return 'Extract the visible text faithfully. Keep original structure when clear, and do not invent missing content.';
}

function normalizeMode(value: unknown): string {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'multimodal' || normalized === 'vision' || normalized === 'vlm') {
        return 'multimodal';
    }
    return 'ocr';
}

function botScope(value: unknown): string {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'followups' || normalized === 'both') {
        return normalized;
    }
    return 'postprocess';
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

function user(value: unknown): string {
    return String(value || '').trim().toLowerCase().replace(/^@+/, '').replace(/\s+/g, '-');
}

function idValue(value: unknown, fallback: string): string {
    const normalized = String(value || '').trim().toLowerCase().replace(/[^a-z0-9-_]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
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
    for (const raw of value.split(',')) {
        const next = lowerCase ? raw.trim().toLowerCase() : raw.trim();
        if (!next || seen.has(next)) {
            continue;
        }
        seen.add(next);
        items.push(next);
    }
    return items;
}

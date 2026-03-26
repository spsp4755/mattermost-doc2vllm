import type {Dispatch, SetStateAction} from 'react';

import type {AdminPluginConfig} from '../client';
import {getAdminConfig} from '../client';
import {buildConfig, draftUsername, loadConfig, normalizeConfig, pickBot, selectionKey} from './config_setting';

jest.mock('manifest', () => ({
    __esModule: true,
    default: {
        id: 'com.mattermost.vllm-llm',
        version: '0.1.0',
    },
}), {virtual: true});

jest.mock('../client', () => ({
    getAdminConfig: jest.fn(),
    getStatus: jest.fn(),
    testConnection: jest.fn(),
}));

const draftConfig: AdminPluginConfig = {
    service: {
        base_url: 'http://localhost:8000/v1/chat/completions',
        auth_mode: 'bearer',
        auth_token: 'secret',
        allow_hosts: 'localhost',
    },
    runtime: {
        default_timeout_seconds: 30,
        enable_streaming: true,
        streaming_update_ms: 800,
        max_input_length: 4000,
        max_output_length: 8000,
        pdf_raster_dpi: 200,
        max_pdf_pages: 20,
        mask_sensitive_data: false,
        enable_debug_logs: false,
        enable_usage_logs: true,
    },
    bots: [
        {
            id: 'doc2vllm-bot-1',
            username: 'doc2vllm-bot-1',
            display_name: 'Bot 1',
            description: 'Test bot',
            model: 'doc2vllm-ocr',
            mode: 'ocr',
            output_mode: 'markdown',
            ocr_prompt: 'Extract faithfully',
            temperature: 0,
            max_tokens: 2048,
            top_p: 1,
            repetition_penalty: 1,
            presence_penalty: 0,
            frequency_penalty: 0,
            extra_request_json: '',
            mask_sensitive_data: false,
            vllm_base_url: '',
            vllm_api_key: '',
            vllm_model: '',
            vllm_prompt: '',
            vllm_scope: 'postprocess',
            allowed_teams: [],
            allowed_channels: [],
            allowed_users: [],
        },
    ],
};

function createSetters() {
    return {
        setConfig: jest.fn() as unknown as Dispatch<SetStateAction<any>>,
        setSource: jest.fn() as unknown as Dispatch<SetStateAction<string>>,
        setSelected: jest.fn() as unknown as Dispatch<SetStateAction<string>>,
        setLoadingConfig: jest.fn() as unknown as Dispatch<SetStateAction<boolean>>,
        setError: jest.fn() as unknown as Dispatch<SetStateAction<string>>,
    };
}

describe('loadConfig', () => {
    beforeEach(() => {
        jest.clearAllMocks();
    });

    test('keeps the editor draft instead of refetching saved config', async () => {
        const raw = JSON.stringify(draftConfig, null, 2);
        const last = {current: raw};
        const setters = createSetters();

        await loadConfig(
            raw,
            last,
            setters.setConfig,
            setters.setSource,
            setters.setSelected,
            setters.setLoadingConfig,
            setters.setError,
        );

        expect(getAdminConfig).not.toHaveBeenCalled();
        expect(setters.setConfig).toHaveBeenCalledTimes(1);
        expect(setters.setSource).toHaveBeenCalledWith('config');
        expect(setters.setLoadingConfig).toHaveBeenCalledWith(false);
        expect(last.current).toBe(raw);
    });

    test('fetches persisted config when the editor value is empty', async () => {
        const last = {current: ''};
        const setters = createSetters();
        (getAdminConfig as jest.Mock).mockResolvedValue({
            config: draftConfig,
            source: 'server',
        });

        await loadConfig(
            '',
            last,
            setters.setConfig,
            setters.setSource,
            setters.setSelected,
            setters.setLoadingConfig,
            setters.setError,
        );

        expect(getAdminConfig).toHaveBeenCalledTimes(1);
        expect(setters.setSource).toHaveBeenCalledWith('server');
        expect(setters.setLoadingConfig).toHaveBeenNthCalledWith(1, true);
        expect(setters.setLoadingConfig).toHaveBeenLastCalledWith(false);
    });

    test('preserves blank bot fields instead of auto-filling them again', () => {
        const config = normalizeConfig({
            ...draftConfig,
            service: {
                ...draftConfig.service,
                base_url: '',
            },
            bots: [{
                ...draftConfig.bots[0],
                username: '',
                display_name: '',
                model: '',
            }],
        });

        const built = buildConfig(config);

        expect(built.service.base_url).toBe('');
        expect(built.bots[0].username).toBe('');
        expect(built.bots[0].display_name).toBe('');
        expect(built.bots[0].model).toBe('');
    });

    test('keeps hyphens while editing the username draft', () => {
        expect(draftUsername('qwen-test-ocr')).toBe('qwen-test-ocr');
        expect(draftUsername('qwen-')).toBe('qwen-');
    });

    test('keeps dots while editing and saving the username', () => {
        expect(draftUsername('qwen.test.ocr')).toBe('qwen.test.ocr');

        const config = normalizeConfig(draftConfig);
        config.bots[0].username = 'qwen.test.ocr';

        const built = buildConfig(config);

        expect(built.bots[0].username).toBe('qwen.test.ocr');
    });

    test('normalizes trailing hyphens only when building the saved config', () => {
        const config = normalizeConfig(draftConfig);
        config.bots[0].username = 'qwen-';

        const built = buildConfig(config);

        expect(built.bots[0].username).toBe('qwen');
    });

    test('keeps the selected bot after the config round-trips through props.value', () => {
        const config = normalizeConfig({
            ...draftConfig,
            bots: [
                draftConfig.bots[0],
                {
                    ...draftConfig.bots[0],
                    id: 'doc2vllm-bot-2',
                    username: 'doc2vllm-bot-2',
                    display_name: 'Bot 2',
                },
                {
                    ...draftConfig.bots[0],
                    id: 'doc2vllm-bot-3',
                    username: '',
                    display_name: '',
                    model: '',
                },
            ],
        });
        config.bots[2].local_id = 'temp-local-3';

        const built = buildConfig(config);
        const roundTripped = normalizeConfig(built);

        expect(pickBot(roundTripped.bots, selectionKey(config.bots[2]))).toBe(selectionKey(roundTripped.bots[2]));
    });

    test('preserves chat mode values in the saved config', () => {
        const config = normalizeConfig({
            ...draftConfig,
            bots: [{
                ...draftConfig.bots[0],
                mode: 'text',
            }],
        });

        const built = buildConfig(config);

        expect(built.bots[0].mode).toBe('chat');
    });
});

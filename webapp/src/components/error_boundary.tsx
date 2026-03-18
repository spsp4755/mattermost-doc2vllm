import React from 'react';

type Props = {
    area: string;
    children: React.ReactNode;
};

type State = {
    hasError: boolean;
    message: string;
};

const containerStyle: React.CSSProperties = {
    background: 'rgba(var(--error-text-color-rgb), 0.08)',
    border: '1px solid rgba(var(--error-text-color-rgb), 0.24)',
    borderRadius: '12px',
    color: 'var(--error-text)',
    display: 'flex',
    flexDirection: 'column',
    gap: '8px',
    padding: '16px',
};

export default class PluginErrorBoundary extends React.PureComponent<Props, State> {
    public state: State = {
        hasError: false,
        message: '',
    };

    public static getDerivedStateFromError(error: Error): State {
        return {
            hasError: true,
            message: error.message || '\uc54c \uc218 \uc5c6\ub294 \uc624\ub958\uac00 \ubc1c\uc0dd\ud588\uc2b5\ub2c8\ub2e4.',
        };
    }

    public componentDidCatch(error: Error, info: React.ErrorInfo) {
        // eslint-disable-next-line no-console
        console.error(`[Doc2VLLM OCR] ${this.props.area} render error`, error, info);
    }

    public render() {
        if (this.state.hasError) {
            return (
                <div style={containerStyle}>
                    <strong>{`${this.props.area} \ud654\uba74\uc744 \ubd88\ub7ec\uc624\uc9c0 \ubabb\ud588\uc2b5\ub2c8\ub2e4.`}</strong>
                    <span>{this.state.message}</span>
                    <span style={{fontSize: '12px', opacity: 0.85}}>
                        {'\ud398\uc774\uc9c0\ub97c \uc0c8\ub85c\uace0\uce68\ud558\uac70\ub098 \ub2e4\uc2dc \uc5f4\uc5b4 \ubcf4\uc138\uc694. \ubb38\uc81c\uac00 \uacc4\uc18d\ub418\uba74 \ud50c\ub7ec\uadf8\uc778 \ub85c\uadf8\uc640 \ube0c\ub77c\uc6b0\uc800 \ucf58\uc194\uc744 \ud568\uaed8 \ud655\uc778\ud574 \uc8fc\uc138\uc694.'}
                    </span>
                </div>
            );
        }

        return this.props.children;
    }
}

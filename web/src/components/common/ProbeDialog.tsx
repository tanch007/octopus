import { useState, type ReactNode } from 'react';
import { CheckCircle2, Loader2, Play, XCircle } from 'lucide-react';
import { useTranslations } from 'use-intl';
import type { ProbeProtocol, ProbeResult } from '@/api/probe';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogDescription,
    MorphingDialogTrigger,
} from '@/components/ui/morphing-dialog';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';

// PROTOCOL_LABELS 是各客户端协议的展示名，与后端 model.ProbeProtocol 一一对应。
const PROTOCOL_LABELS: Record<ProbeProtocol, string> = {
    chat: 'Chat Completions',
    response: 'Responses',
    anthropic: 'Anthropic Messages',
    embedding: 'Embeddings',
};

export type ProbeDialogProps = {
    // 弹窗标题, 区分模型测试与分组测试。
    title: string;
    // 目标说明: 测的是哪个渠道的哪个模型, 或是哪个分组。
    target: string;
    // 可选的协议: 由调用方给出授权实际支持的协议, 用户不会选到无法承接的协议。
    protocols: ProbeProtocol[];
    // 可选的凭据: 模型测试要指定用哪一份凭据; 分组测试由路由决定用哪一份, 不给这项。
    keys?: string[];
    // 触发一次探测, 返回探测结果。
    onProbe: (input: { protocol: ProbeProtocol; key: string; prompt: string }) => Promise<ProbeResult>;
    // 触发按钮, 由调用方给出图标与样式。
    trigger: ReactNode;
};

// ProbeDialog 是模型测试与分组测试共用的探测弹窗。
// 两种探测的差别只在发往哪个接口, 故弹窗只负责收集协议与提示词, 并把结果摊开给人看:
// 命中了谁, 用了哪个协议, 花了多久, 返回了什么, 或者错在哪里。
export function ProbeDialog({ title, target, protocols, keys, onProbe, trigger }: ProbeDialogProps) {
    const t = useTranslations('probe');
    const [protocol, setProtocol] = useState<ProbeProtocol>(protocols[0] ?? 'chat');
    const [key, setKey] = useState(keys?.[0] ?? '');
    const [prompt, setPrompt] = useState('');
    const [result, setResult] = useState<ProbeResult | null>(null);
    const [pending, setPending] = useState(false);
    const [showRaw, setShowRaw] = useState(false);

    // 协议与凭据候选随授权变化: 授权改过之后旧选项可能已不被支持, 故以传入的候选为准。
    const available = protocols.length > 0 ? protocols : (['chat'] as ProbeProtocol[]);
    const current = available.includes(protocol) ? protocol : available[0];
    const availableKeys = keys && keys.length > 0 ? keys : [];
    const currentKey = availableKeys.includes(key) ? key : availableKeys[0] ?? '';

    const run = async () => {
        setPending(true);
        try {
            setResult(await onProbe({ protocol: current, key: currentKey, prompt }));
        } catch {
            // 探测接口本身失败(多为网络或鉴权问题), 与上游失败不同: 这里没有结果可展示。
            setResult(null);
        } finally {
            setPending(false);
        }
    };

    return (
        <MorphingDialog>
            <MorphingDialogTrigger>{trigger}</MorphingDialogTrigger>
            <MorphingDialogContainer>
                <MorphingDialogContent className="relative w-screen max-w-full md:max-w-2xl bg-card text-card-foreground rounded-3xl overflow-hidden">
                    <MorphingDialogDescription className="flex flex-col gap-4 p-6">
                        <header className="flex flex-col gap-1">
                            <h2 className="text-lg font-bold">{title}</h2>
                            <p className="text-sm text-muted-foreground break-all">{target}</p>
                        </header>

                        <div className="grid gap-3 sm:grid-cols-2">
                            <div className="flex flex-col gap-2">
                                <Label htmlFor="probe-protocol">{t('protocol')}</Label>
                                <Select value={current} onValueChange={(value) => setProtocol(value as ProbeProtocol)}>
                                    <SelectTrigger id="probe-protocol" className="w-full">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {available.map((item) => (
                                            <SelectItem key={item} value={item}>
                                                {PROTOCOL_LABELS[item]}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                            {availableKeys.length > 0 && (
                                <div className="flex flex-col gap-2">
                                    <Label htmlFor="probe-key">{t('key')}</Label>
                                    <Select value={currentKey} onValueChange={setKey}>
                                        <SelectTrigger id="probe-key" className="w-full">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {availableKeys.map((item) => (
                                                <SelectItem key={item} value={item}>
                                                    {item}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>
                            )}
                        </div>

                        <div className="flex flex-col gap-2">
                            <Label htmlFor="probe-prompt">{t('prompt')}</Label>
                            <Input
                                id="probe-prompt"
                                value={prompt}
                                placeholder={t('promptPlaceholder')}
                                onChange={(event) => setPrompt(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === 'Enter' && !pending) run();
                                }}
                            />
                        </div>

                        <div className="flex items-center gap-2">
                            <Button type="button" onClick={run} disabled={pending}>
                                {pending ? <Loader2 className="animate-spin" /> : <Play />}
                                {pending ? t('running') : t('run')}
                            </Button>
                        </div>

                        {result && (
                            <section className="flex flex-col gap-3 rounded-2xl border border-border/70 bg-muted/30 p-4">
                                <div className="flex items-center gap-2">
                                    {result.ok ? (
                                        <CheckCircle2 className="size-4 shrink-0 text-emerald-500" />
                                    ) : (
                                        <XCircle className="size-4 shrink-0 text-destructive" />
                                    )}
                                    <span className="text-sm font-semibold">
                                        {result.ok ? t('success') : t('failed')}
                                    </span>
                                    <span className="text-xs text-muted-foreground tabular-nums">
                                        {result.status_code > 0 ? `${result.status_code} · ` : ''}
                                        {result.duration_ms} ms
                                    </span>
                                </div>

                                <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-4">
                                    <div className="flex flex-col">
                                        <dt className="text-muted-foreground">{t('channel')}</dt>
                                        <dd className="truncate font-medium">{result.channel_name || '-'}</dd>
                                    </div>
                                    <div className="flex flex-col">
                                        <dt className="text-muted-foreground">{t('model')}</dt>
                                        <dd className="truncate font-medium">{result.model_name || '-'}</dd>
                                    </div>
                                    <div className="flex flex-col">
                                        <dt className="text-muted-foreground">{t('key')}</dt>
                                        <dd className="truncate font-medium">{result.key_name || '-'}</dd>
                                    </div>
                                    <div className="flex flex-col">
                                        <dt className="text-muted-foreground">{t('upstreamProtocol')}</dt>
                                        <dd className="truncate font-medium">
                                            {PROTOCOL_LABELS[result.protocol as ProbeProtocol] ?? (result.protocol || '-')}
                                        </dd>
                                    </div>
                                </dl>

                                {(result.input_tokens > 0 || result.output_tokens > 0) && (
                                    <div className="text-xs text-muted-foreground tabular-nums">
                                        {t('tokens', {
                                            input: result.input_tokens,
                                            output: result.output_tokens,
                                        })}
                                    </div>
                                )}

                                {result.error && (
                                    <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-destructive/10 p-3 text-xs text-destructive">
                                        {result.error}
                                    </pre>
                                )}

                                {result.content && (
                                    <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-background p-3 text-xs">
                                        {result.content}
                                    </pre>
                                )}

                                {result.raw_response && (
                                    <div className="flex flex-col gap-1">
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="xs"
                                            className="self-start"
                                            onClick={() => setShowRaw(!showRaw)}
                                        >
                                            {showRaw ? t('hideRaw') : t('showRaw')}
                                        </Button>
                                        {showRaw && (
                                            <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-background p-3 text-xs text-muted-foreground">
                                                {result.raw_response}
                                            </pre>
                                        )}
                                    </div>
                                )}
                            </section>
                        )}
                    </MorphingDialogDescription>
                </MorphingDialogContent>
            </MorphingDialogContainer>
        </MorphingDialog>
    );
}

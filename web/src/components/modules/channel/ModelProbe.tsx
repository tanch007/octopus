import { useMemo } from 'react';
import { FlaskConical } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { OpenAIDialogs, Protocol } from '@/api/channel';
import { useProbeModel, type ProbeProtocol } from '@/api/probe';
import { ProbeDialog } from '@/components/common/ProbeDialog';
import { IconButton } from '@/components/common/IconButton';
import { grantKey, type ChannelFormState } from './state';

// DIALOG_PROBES 是对话类协议的探测候选: 三者之间可经转换互相承接,
// 故授权只要声明了其中任意一个, 三种都能测; 实际发往上游的协议由授权决定。
const DIALOG_PROBES: ProbeProtocol[] = ['chat', 'response', 'anthropic'];

// EMBEDDING_PROBE 是嵌入协议的探测候选: 媒体类协议没有转换伙伴, 只能精确匹配授权声明的那一个。
const EMBEDDING_PROBE: ProbeProtocol = 'embedding';

// ModelProbeButton 是模型行上的测试入口: 直连该模型在其授权凭据上的上游, 看能否打通。
// 渠道尚未保存时没有主键, 后端无从定位, 故此时不给入口。
export function ModelProbeButton({ channelId, state, modelName }: {
    channelId?: number;
    state: ChannelFormState;
    modelName: string;
}) {
    const t = useTranslations('probe');
    const probeModel = useProbeModel();

    // 候选凭据是该模型已授权的凭据, 协议候选由这些授权声明的协议集合推出。
    const { protocols, keys } = useMemo(() => {
        const names: string[] = [];
        let bits = 0;
        for (const key of state.keys) {
            const granted = state.grants.get(grantKey(modelName, key.name)) ?? 0;
            if (granted === 0) continue;
            names.push(key.name);
            bits |= granted;
        }
        const options: ProbeProtocol[] = [];
        if (bits & OpenAIDialogs) options.push(...DIALOG_PROBES);
        if (bits & Protocol.OpenAIEmbedding) options.push(EMBEDDING_PROBE);
        return { protocols: options, keys: names };
    }, [state.keys, state.grants, modelName]);

    const disabled = channelId === undefined || protocols.length === 0;

    return (
        <ProbeDialog
            title={t('modelTitle')}
            target={`${state.name} · ${modelName}`}
            protocols={protocols}
            keys={keys}
            onProbe={({ protocol, key, prompt }) =>
                probeModel.mutateAsync({
                    channel_id: channelId as number,
                    model_name: modelName,
                    key_name: key,
                    protocol,
                    prompt,
                })
            }
            trigger={
                <IconButton
                    className="size-7"
                    disabled={disabled}
                    tip={channelId === undefined ? t('modelNeedSaved') : t('modelTest')}
                >
                    <FlaskConical className="size-3.5" />
                </IconButton>
            }
        />
    );
}

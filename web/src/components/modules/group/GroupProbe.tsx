import { useMemo } from 'react';
import { FlaskConical } from 'lucide-react';
import { useTranslations } from 'use-intl';
import { OpenAIDialogs, Protocol } from '@/api/channel';
import { type Group } from '@/api/group';
import { useProbeGroup, type ProbeProtocol } from '@/api/probe';
import { ProbeDialog } from '@/components/common/ProbeDialog';
import { IconButton } from '@/components/common/IconButton';

// DIALOG_PROBES 是对话类协议的探测候选: 三者之间可经转换互相承接,
// 故成员只要声明了其中任意一个, 三种都能测; 实际发往上游的协议由成员授权决定。
const DIALOG_PROBES: ProbeProtocol[] = ['chat', 'response', 'anthropic'];

// EMBEDDING_PROBE 是嵌入协议的探测候选: 媒体类协议没有转换伙伴, 只能精确匹配成员授权声明的那一个。
const EMBEDDING_PROBE: ProbeProtocol = 'embedding';

// GroupProbeButton 是分组卡片上的测试入口: 按分组的路由规则挑一个成员来测, 看真实请求此刻会落到谁身上。
// 探测只读: 不改当前成员, 不让失败的成员进入冷却, 也不计入统计。
export function GroupProbeButton({ group }: { group: Group }) {
    const t = useTranslations('probe');
    const probeGroup = useProbeGroup();

    // 候选协议由成员授权声明的协议集合推出: 成员各自声明的协议取并集。
    const protocols = useMemo(() => {
        let bits = 0;
        for (const item of group.items || []) bits |= item.protocols;
        const options: ProbeProtocol[] = [];
        if (bits & OpenAIDialogs) options.push(...DIALOG_PROBES);
        if (bits & Protocol.OpenAIEmbedding) options.push(EMBEDDING_PROBE);
        return options;
    }, [group.items]);

    return (
        <ProbeDialog
            title={t('groupTitle')}
            target={group.name}
            protocols={protocols}
            onProbe={({ protocol, prompt }) =>
                probeGroup.mutateAsync({ group_id: group.id, protocol, prompt })
            }
            trigger={
                <IconButton asChild className="size-7">
                    <FlaskConical className="size-4" />
                </IconButton>
            }
        />
    );
}

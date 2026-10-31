import { useMutation } from '@tanstack/react-query';
import { apiRequest } from './client';

// ProbeProtocol 是探测请求使用的客户端协议，与后端 model.ProbeProtocol 一致。
// 客户端协议只决定请求的形态，实际发往上游的协议由渠道授权声明的协议集合决定。
export type ProbeProtocol = 'chat' | 'response' | 'anthropic' | 'embedding';

// ProbeResult 是一次探测的结果。
// 探测不计统计也不写日志，故这里只有本次调用的即时结果；失败时 error 与 raw_response 一并给出。
export type ProbeResult = {
    ok: boolean;
    channel_id: number;
    channel_name: string;
    model_name: string;
    key_name: string;
    protocol: string; // 实际发往上游的协议。
    client_protocol: string; // 探测请求使用的客户端协议。
    status_code: number;
    duration_ms: number;
    input_tokens: number;
    output_tokens: number;
    content: string;
    raw_response: string;
    error: string;
};

// ProbeModelRequest 是模型探测请求：指定渠道下的某个模型与凭据。
// 模型与凭据按名称定位，与界面上的读法一致，前端无需持有它们的主键。
export type ProbeModelRequest = {
    channel_id: number;
    model_name: string;
    key_name: string;
    protocol?: ProbeProtocol;
    prompt?: string;
};

// ProbeGroupRequest 是分组探测请求：按分组的路由规则挑一个成员来测。
export type ProbeGroupRequest = {
    group_id: number;
    protocol?: ProbeProtocol;
    prompt?: string;
};

/**
 * 模型探测 Hook：直连指定渠道下的某个模型与凭据测试上游。
 *
 * @example
 * const probeModel = useProbeModel();
 *
 * probeModel.mutate({ channel_id: 1, model_name: 'gpt-4o', key_name: 'default', protocol: 'chat' });
 */
export function useProbeModel() {
    return useMutation({
        mutationFn: (data: ProbeModelRequest) =>
            apiRequest<ProbeResult>('/api/v1/probe/model', { method: 'POST', body: data }),
    });
}

/**
 * 分组探测 Hook：按分组的路由规则挑一个成员来测，看真实请求此刻会落到谁身上。
 *
 * @example
 * const probeGroup = useProbeGroup();
 *
 * probeGroup.mutate({ group_id: 1, protocol: 'chat' });
 */
export function useProbeGroup() {
    return useMutation({
        mutationFn: (data: ProbeGroupRequest) =>
            apiRequest<ProbeResult>('/api/v1/probe/group', { method: 'POST', body: data }),
    });
}

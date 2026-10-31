#!/usr/bin/env python3
"""本地模拟上游, 用于调试 Octopus 的转发链路。

它会响应嵌入, 语音合成, 语音转写, 语音翻译与视频五个端点, 并把网关发来的
每一个请求原样打印到标准输出 —— 打印内容里的模型名与表单结构, 正是排查
"模型名有没有被替换" "多段表单有没有被改坏" 这类问题的依据。

用法:
    python3 scripts/dev/mock-upstream.py [--port 8899]

然后在渠道里把 Base URL 填成 http://127.0.0.1:8899, 并按需勾选协议位。
"""

import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

# 模拟上游对外声称的模型名; 网关应把分组名替换成它。
MODEL_NAME = "mock-upstream-model"


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass  # 请求日志由 dump 统一打印。

    def dump(self, body: bytes):
        """打印收到的请求, 便于核对网关实际发往上游的内容。"""
        print(f"\n>>> {self.command} {self.path}")
        for key in ("Content-Type", "Authorization", "Accept"):
            value = self.headers.get(key)
            if value:
                print(f"    {key}: {value}")
        if body:
            try:
                text = json.loads(body)
                print("    body:", json.dumps(text, ensure_ascii=False))
            except (ValueError, UnicodeDecodeError):
                print(f"    body: <{len(body)} bytes 二进制>")
        sys.stdout.flush()  # 实时输出, 否则重定向到文件时会被缓冲挡住。

    def reply(self, payload, status=200, content_type="application/json"):
        body = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_body(self):
        length = int(self.headers.get("Content-Length", 0))
        return self.rfile.read(length) if length else b""

    def do_POST(self):
        body = self.read_body()
        self.dump(body)

        if self.path.endswith("/embeddings"):
            return self.reply({
                "object": "list",
                "data": [{"object": "embedding", "embedding": [0.1, 0.2, 0.3], "index": 0}],
                "model": MODEL_NAME,
                "usage": {"prompt_tokens": 5, "total_tokens": 5},
            })

        if self.path.endswith("/audio/speech"):
            # 语音合成返回音频二进制; 真实场景是 mp3, 这里给一段可识别的占位字节。
            return self.reply(b"\xff\xfb\x90\x00mock-audio-bytes", content_type="audio/mpeg")

        if self.path.endswith("/audio/transcriptions") or self.path.endswith("/audio/translations"):
            return self.reply({"text": "这是一段模拟的转写文本"})

        if self.path.endswith("/videos"):
            return self.reply({
                "id": "video_mock_001",
                "object": "video",
                "status": "queued",
                "model": MODEL_NAME,
            })

        if self.path.endswith("/chat/completions"):
            return self.reply({
                "id": "chatcmpl-mock",
                "object": "chat.completion",
                "model": MODEL_NAME,
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": "mock reply"},
                    "finish_reason": "stop",
                }],
                "usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
            })

        return self.reply({"error": {"message": f"mock upstream has no route for {self.path}"}}, status=404)

    def do_GET(self):
        self.dump(b"")
        if "/videos/" in self.path:
            task_id = self.path.rsplit("/", 1)[-1]
            return self.reply({
                "id": task_id,
                "object": "video",
                "status": "in_progress",
                "progress": 0.5,
                "model": MODEL_NAME,
            })
        return self.reply({"error": {"message": f"mock upstream has no route for {self.path}"}}, status=404)

    def do_DELETE(self):
        self.dump(b"")
        if "/videos/" in self.path:
            task_id = self.path.rsplit("/", 1)[-1]
            return self.reply({"id": task_id, "object": "video", "status": "cancelled"})
        return self.reply({"error": {"message": f"mock upstream has no route for {self.path}"}}, status=404)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Octopus 本地模拟上游")
    parser.add_argument("--port", type=int, default=8899, help="监听端口, 默认 8899")
    parser.add_argument("--host", default="127.0.0.1", help="监听地址, 默认 127.0.0.1")
    args = parser.parse_args()

    print(f"mock upstream 已启动: http://{args.host}:{args.port}  (模型名 {MODEL_NAME})")
    HTTPServer((args.host, args.port), Handler).serve_forever()

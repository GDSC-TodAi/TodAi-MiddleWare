#!/usr/bin/env python3
"""
TodAi 시연용 Mock 서버
- STT  mock: port 9001  (POST /transcribe → {"text": "..."})
- TTS  mock: port 9002  (POST /synthesize → raw PCM bytes)
- LLM  mock: port 11435 (POST /api/generate → {"response": "..."})

사용법:
  python3 test/mock_servers.py

.env 혹은 test/demo.env 에 아래 설정 필요:
  STT_BASE_URL=http://localhost:9001
  TTS_BASE_URL=http://localhost:9002
  LLM_BASE_URL=http://localhost:11435
"""

import http.server
import json
import math
import struct
import threading
import itertools
import time

# ── 샘플 대화 목록 (순환) ──────────────────────────────────────────────────────
STT_PHRASES = itertools.cycle([
    "오늘 날씨가 참 좋네요",
    "요즘 허리가 많이 아파요",
    "아들이 요즘 통 연락이 없어요",
    "어제 산책을 나갔다가 이웃을 만났어요",
    "밥은 먹었는데 입맛이 없어요",
    "무릎이 시려서 외출하기가 무서워요",
    "요즘 잠이 잘 안 와요",
])

LLM_RESPONSES = itertools.cycle([
    "네, 정말 좋은 날씨예요. 잠깐 창문을 열어 바람 좀 맞으시겠어요?",
    "허리가 많이 불편하시겠어요. 따뜻하게 찜질도 해보시고 무리하지 마세요.",
    "아드님이 바쁘신가 봐요. 곧 연락이 올 거예요, 너무 걱정 마세요.",
    "이웃분들과 좋은 시간 보내셨겠네요. 이런 만남이 참 좋죠.",
    "입맛이 없을 때는 좋아하시는 음식 조금이라도 드셔보세요.",
    "날이 추우면 무릎이 더 시리죠. 따뜻하게 입고 나가세요.",
    "잠이 잘 안 오실 때는 따뜻한 우유 한 잔이 도움이 된답니다.",
])

# ── TTS: 사인파 오디오 생성 (440Hz, 1초, 16kHz, PCM 16-bit mono) ──────────────

def generate_sine_wave(freq=440.0, duration=1.0, sample_rate=16000, amplitude=0.25):
    n = int(sample_rate * duration)
    # fade in/out 100ms to avoid click
    fade = int(sample_rate * 0.1)
    data = []
    for i in range(n):
        raw = amplitude * math.sin(2 * math.pi * freq * i / sample_rate)
        # envelope
        if i < fade:
            raw *= i / fade
        elif i > n - fade:
            raw *= (n - i) / fade
        pcm = max(-32768, min(32767, int(raw * 32767)))
        data.append(struct.pack('<h', pcm))
    return b''.join(data)

AUDIO_CACHE = generate_sine_wave()  # pre-generate once


# ── 공통 핸들러 베이스 ─────────────────────────────────────────────────────────

class BaseHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass  # suppress default access log (Go server's log is enough)

    def send_json(self, obj, status=200):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_body(self):
        length = int(self.headers.get('Content-Length', 0))
        return self.rfile.read(length) if length else b''


# ── STT 핸들러 ─────────────────────────────────────────────────────────────────

class STTHandler(BaseHandler):
    def do_POST(self):
        if self.path != '/transcribe':
            self.send_response(404); self.end_headers(); return
        self.read_body()
        text = next(STT_PHRASES)
        time.sleep(0.2)  # simulate processing delay
        self.send_json({'text': text})
        print(f'[STT]  "{text}"')


# ── TTS 핸들러 ─────────────────────────────────────────────────────────────────

class TTSHandler(BaseHandler):
    def do_POST(self):
        if self.path != '/synthesize':
            self.send_response(404); self.end_headers(); return
        body = self.read_body()
        text = json.loads(body).get('text', '') if body else ''
        time.sleep(0.15)  # simulate processing delay
        audio = AUDIO_CACHE
        self.send_response(200)
        self.send_header('Content-Type', 'application/octet-stream')
        self.send_header('Content-Length', str(len(audio)))
        self.end_headers()
        self.wfile.write(audio)
        print(f'[TTS]  "{text[:40]}" → {len(audio)} bytes')


# ── LLM 핸들러 (Ollama 호환) ───────────────────────────────────────────────────

class LLMHandler(BaseHandler):
    def do_POST(self):
        if self.path != '/api/generate':
            self.send_response(404); self.end_headers(); return
        body = self.read_body()
        req = json.loads(body) if body else {}
        prompt = req.get('prompt', '')
        time.sleep(0.5)  # simulate LLM latency
        response = next(LLM_RESPONSES)
        self.send_json({'response': response, 'done': True})
        print(f'[LLM]  "{prompt[:30]}..." → "{response}"')


# ── 서버 실행 ──────────────────────────────────────────────────────────────────

def run_server(handler_class, port):
    server = http.server.HTTPServer(('0.0.0.0', port), handler_class)
    print(f'[mock] {handler_class.__name__} listening on :{port}')
    server.serve_forever()


if __name__ == '__main__':
    servers = [
        # STT는 test/stt_server.py 가 담당 (실제 Whisper)
        (TTSHandler, 9002),
        (LLMHandler, 11435),
    ]
    threads = []
    for handler, port in servers:
        t = threading.Thread(target=run_server, args=(handler, port), daemon=True)
        t.start()
        threads.append(t)

    print('\nTodAi mock servers running. Ctrl+C to stop.\n')
    try:
        for t in threads:
            t.join()
    except KeyboardInterrupt:
        print('\nStopped.')

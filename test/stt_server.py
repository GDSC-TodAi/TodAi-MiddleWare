#!/usr/bin/env python3
"""
TodAi 실제 STT 서버 (port 9001)
faster-whisper tiny 모델 사용, 한국어 최적화

설치:
  pip install faster-whisper

실행:
  python3 test/stt_server.py

계약: POST /transcribe — body: raw PCM 16-bit 16kHz mono bytes
       응답: {"text": "..."}
"""

import http.server
import json
import numpy as np
import time

try:
    from faster_whisper import WhisperModel
except ImportError:
    print("ERROR: faster-whisper not installed.")
    print("  pip install faster-whisper")
    raise SystemExit(1)

# 모델 로딩 (최초 1회 다운로드 ~150MB)
print("[STT] loading whisper tiny model...")
MODEL = WhisperModel("tiny", device="cpu", compute_type="int8")
print("[STT] model ready")


class STTHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def do_POST(self):
        if self.path != '/transcribe':
            self.send_response(404)
            self.end_headers()
            return

        length = int(self.headers.get('Content-Length', 0))
        pcm_bytes = self.rfile.read(length) if length else b''

        if not pcm_bytes:
            self._send_json({'text': ''})
            return

        t0 = time.time()

        # PCM 16-bit → float32 [-1, 1]
        audio = np.frombuffer(pcm_bytes, dtype=np.int16).astype(np.float32) / 32768.0

        segments, _ = MODEL.transcribe(
            audio,
            language='ko',
            beam_size=1,         # 빠른 추론
            vad_filter=False,    # Go 미들웨어에서 이미 VAD 처리
        )
        text = ''.join(s.text for s in segments).strip()
        elapsed = int((time.time() - t0) * 1000)

        print(f'[STT] "{text}" ({elapsed}ms, {len(pcm_bytes)//2} samples)')
        self._send_json({'text': text})

    def _send_json(self, obj):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == '__main__':
    server = http.server.HTTPServer(('0.0.0.0', 9001), STTHandler)
    print('[STT] listening on :9001')
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print('\n[STT] stopped.')

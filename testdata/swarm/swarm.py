#!/usr/bin/env python3
"""A loopback-only BitTorrent v1 tracker and single-file seeder."""
import hashlib
import http.server
import pathlib
import socketserver
import struct
import threading
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parent
DATA = (ROOT / 'sample.mp4').read_bytes()
SIZE = 16384

def bencode(value):
    if isinstance(value, int): return b'i%de' % value
    if isinstance(value, bytes): return str(len(value)).encode() + b':' + value
    if isinstance(value, dict): return b'd' + b''.join(bencode(k) + bencode(value[k]) for k in sorted(value)) + b'e'
    raise TypeError(value)

INFO = {b'length': len(DATA), b'name': b'sample.mp4', b'piece length': SIZE,
        b'pieces': b''.join(hashlib.sha1(DATA[i:i+SIZE]).digest() for i in range(0, len(DATA), SIZE))}
HASH = hashlib.sha1(bencode(INFO)).digest()
PEER_ID = b'-TP0001-012345678901'

def read_exact(sock, n):
    result = b''
    while len(result) < n:
        chunk = sock.recv(n - len(result))
        if not chunk: raise EOFError()
        result += chunk
    return result

def send(sock, ident, payload=b''):
    sock.sendall(struct.pack('!I', 1 + len(payload)) + bytes([ident]) + payload)

class Peer(socketserver.BaseRequestHandler):
    def handle(self):
        s = self.request
        s.settimeout(10)
        try:
            handshake = read_exact(s, 68)
            if handshake[:20] != b'\x13BitTorrent protocol' or handshake[28:48] != HASH: return
            s.sendall(b'\x13BitTorrent protocol' + bytes(8) + HASH + PEER_ID)
            count = (len(DATA) + SIZE - 1) // SIZE
            bits = bytearray((count + 7) // 8)
            for i in range(count): bits[i // 8] |= 128 >> (i % 8)
            send(s, 5, bits)
            while True:
                length = struct.unpack('!I', read_exact(s, 4))[0]
                if length > 1024: return
                if not length: continue
                msg = read_exact(s, length)
                if msg[0] == 2: send(s, 1)
                elif msg[0] == 6 and len(msg) == 13:
                    index, begin, size = struct.unpack('!III', msg[1:])
                    if index >= count or size == 0 or size > 16384 or begin + size > min(SIZE, len(DATA) - index * SIZE): return
                    send(s, 7, struct.pack('!II', index, begin) + DATA[index*SIZE+begin:index*SIZE+begin+size])
        except (EOFError, OSError): pass

class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

class Tracker(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        query = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query, encoding='latin1')
        if urllib.parse.urlsplit(self.path).path != '/announce' or query.get('info_hash', [''])[0].encode('latin1') != HASH:
            body = bencode({b'failure reason': b'unknown torrent'})
        else:
            body = bencode({b'interval': 60, b'peers': b'\x7f\x00\x00\x01' + struct.pack('!H', 6881)})
        self.send_response(200); self.send_header('Content-Length', str(len(body))); self.end_headers(); self.wfile.write(body)
    def log_message(self, fmt, *args): pass

if __name__ == '__main__':
    torrent = bencode({b'announce': b'http://127.0.0.1:8080/announce', b'info': INFO})
    (ROOT / 'sample.torrent').write_bytes(torrent)
    with Server(('127.0.0.1', 6881), Peer) as peer, http.server.ThreadingHTTPServer(('127.0.0.1', 8080), Tracker) as tracker:
        threading.Thread(target=peer.serve_forever, daemon=True).start()
        print('Tracker: http://127.0.0.1:8080/announce', flush=True)
        print('SHA-256: ' + hashlib.sha256(DATA).hexdigest(), flush=True)
        print('Info hash: ' + HASH.hex(), flush=True)
        try: tracker.serve_forever()
        except KeyboardInterrupt: pass
        finally: peer.shutdown()

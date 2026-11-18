# Local swarm fixture

Requires Python 3. Start with `python3 testdata/swarm/swarm.py` from the repository root. It writes `sample.torrent` beside the sample and runs a tracker at `http://127.0.0.1:8080/announce` and a TCP seeder at `127.0.0.1:6881`. Stop with Ctrl-C and remove `testdata/swarm/sample.torrent` when finished. Both ports must be free. All traffic stays on loopback.

The sample is a two-second synthetic FFmpeg test pattern, generated for this project and redistributable. Its SHA-256 is `18d449beee066d85ea790ec17f4f82df64d03f6deb7573b6a5b82c45ffc0c796`. It was generated with `ffmpeg -f lavfi -i 'testsrc=size=128x96:rate=10:duration=2' -c:v libx264 -pix_fmt yuv420p -movflags +faststart -an sample.mp4`.

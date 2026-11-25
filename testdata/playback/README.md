# Playback fixtures

`multikeyframe.mp4` is the same synthetic test pattern as the local swarm
sample, but re-encoded with a keyframe every five frames so that fragment
boundaries can be checked. It is 64x48, 10 frames per second, two seconds long
and contains 20 frames and four keyframes, so remuxing it must produce four
fragments.

Generated with:

```
ffmpeg -f lavfi -i 'testsrc=size=64x48:rate=10:duration=2' \
  -c:v libx264 -g 5 -keyint_min 5 -sc_threshold 0 \
  -pix_fmt yuv420p -movflags +faststart -an multikeyframe.mp4
```

SHA-256: `1697f92cafdb1e6d9f28e047c320648737e2bd4b86fc0ee7b41644f4f0d69dd8`

Test streams are served over a loopback unix domain socket only; the mpv
integration test runs without a window and without audio output.

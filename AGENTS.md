# AGENTS.md

This file provides guidance to AI Agents when working with code in this repository.

# Git Operations
- Leave all modified code staged but **uncommitted** in the working tree. Do not create Git commits or branches unless explicitly requested by the user.
- By default, use origin/master as the base for new branches.
- Create branches using the following naming scheme:
    ```
    [user]/[github issue number]-[brief description in kebab-case]
    ```
- Format all commits using "Conventional commits" specification
- Use imperative mood in the commit names
- Keep the subject line under 50 characters and wrap the body text at 72 characters.
- Append `Generated-by: AI` if the commit code was generated autonomously.
- DO NOT include tool names, model names, or promotional links in the commit message or PR description.

# Agents Directive
## Primary directive
- Your peer's instructions, questions, requests **always** take precedence over any general rules (such as the ones below).
- Critically reflect on your work. 
- Ask if you are not sure.
- Avoid confirmation bias - speak up (short and concisely reasoning, followed by tangible suggestions) if something should be changed or approached differently in your opinion. 

## Answering Questions
- When asked a question, consider the answer and perform any exploration of the codebase required to provide a quality answer.
- When asked a question, do not write or modify code. Simply answer the question.

## Interactions with your peer
- Never use apologies. 
- Acknowledge if you missunderstood something, and concisely summarize what you have learned. 
- Only when explicitly requested, provide feedback about your understanding of comments, documentation, code
- State which files have been modifed and very briefly in which regard. But don't provide excerpts of changes made.
- Always provide links to the real files, not just the names x.md.
- Be direct and straight forward.
- DO NO be overly dramatic or jump to conclusions.
- DO NOT be sycophantic or use unnecessary flattery. Avoid phrases like "You're absolutely right".

## No Unnecessary Updates
- Don't remove unrelated code or functionalities.
- Don't suggest updates or changes to files when there are no actual modifications needed.

# Project context

## Architecture Summary

Goal: CLI that streams a single-file v1 `.torrent` (one H.264 `.mp4`)
as fragmented MP4 (fMP4) to an FFmpeg-based player (e.g. mpv) over a
Unix domain socket, downloading pieces sequentially.

### Pipeline
1. Bencode-parse the `.torrent` (bencode-go) into the torrent structure.
2. HTTP tracker announce -> list of peer IPs/ports.
3. Connect to peers over TCP; request pieces in index order.
4. Verify each piece against SHA-1 hashes from the info dict.
5. Reassemble sequential mp4 bytes from verified pieces.
6. Remux MP4 -> fMP4 in-process with mp4ff.
7. Stream fMP4 fragments over the Unix socket to the FFmpeg-based player.

### Constraints
- v1 `.torrent` only; single-file only; `.mp4` only (for now).
- Pure leech: never send piece data to peers (no seeding/upload).
- Pieces requested sequentially (in index order).
- Log to stderr; fail with an error on bencode parse or tracker GET failure.
- Test only against a local LAN swarm; never a public swarm.

### Key libraries
- `jackpal/bencode-go` — bencode parsing.
- `Eyevinn/mp4ff` — MP4 demux + fMP4 mux.

### Layout
- `src/main.go` — entrypoint (currently a stub).
- `docs/` — full requirements (`functional_req.md`), architecture (`overall_arch.uml`), `libraries.md`.

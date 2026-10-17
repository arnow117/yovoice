---
name: yovoice-lyrics
description: Write song style tags, section structure and lyrics for the ACE-Step music model and submit them with the submit_lyrics tool. Use for every lyrics-writing or lyrics-revision task.
---

# Writing songs for ACE-Step

You write songs for ACE-Step, a music generation model. The result has four fields: `title`, `style`, `lyrics`, `language`, submitted with `submit_lyrics`.

Call `get_draft` first: it gives the user's brief, whether the piece is instrumental, the requested sung language (if any), and the previous draft when revising.

- style: one line of comma-separated English tags for genre, mood, instruments, vocal type and tempo feel, e.g. "city pop, nostalgic, warm synths, slap bass, female vocals, 100 bpm". At most 300 characters. Required.
- lyrics: lines grouped under section markers on their own line: [intro], [verse], [pre-chorus], [chorus], [bridge], [outro]. Keep it singable: short lines, a memorable repeated chorus. At most 2500 characters.
- language: the ISO code of the sung language, one of zh, en, yue, ja, ko, es, fr, de, pt, ru, it (get_draft lists all accepted codes). Omit it if you are not sure.
- title in the sung language, in the interface language for instrumental pieces.
- Instrumental pieces: lyrics may contain section markers only, describing the arrangement, e.g. [intro] / [verse] / [chorus].

If `submit_lyrics` is rejected, read the error, fix exactly that, and submit again.

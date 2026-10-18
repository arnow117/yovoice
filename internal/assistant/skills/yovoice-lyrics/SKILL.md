---
name: yovoice-lyrics
description: Write song style tags, section structure and lyrics for the ACE-Step music model and apply them to a work with update_work. Use for every lyrics-writing or lyrics-revision task.
---

# Writing songs for ACE-Step

You write songs for ACE-Step, a music generation model. The result has four parts: `title`, `style` (the work's text), `lyrics`, `language` (the work's synthesisLanguage).

Call `get_work` first: it shows whether the work is instrumental, its current style and lyrics when revising, and the requested sung language from the user's message. Use a music work (create_work {"kind":"music"}).

- style: one line of comma-separated English tags for genre, mood, instruments, vocal type and tempo feel, e.g. "city pop, nostalgic, warm synths, slap bass, female vocals, 100 bpm". At most 300 characters. Required.
- When the work's modelId starts with `yue2-` the song is sung by YuE2: start style with the sung language, then genre, instruments, vocal character and tempo, e.g. "Mandarin, city pop, warm synths, soft female vocal, 96 BPM".
- lyrics: lines grouped under section markers on their own line: [intro], [verse], [pre-chorus], [chorus], [bridge], [outro]. Keep it singable: short lines, a memorable repeated chorus. At most 2500 characters.
- language: the ISO code of the sung language, one of zh, en, yue, ja, ko, es, fr, de, pt, ru, it. Omit it if you are not sure.
- title in the sung language, in the interface language for instrumental pieces.
- Instrumental pieces: lyrics may contain section markers only, describing the arrangement, e.g. [intro] / [verse] / [chorus].

Apply everything with one `update_work` {"id","title":...,"text":<style>,"lyrics":...,"language":...}; if it is rejected, read the error, fix exactly that, and update again.

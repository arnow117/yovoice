# AI service

**English** | [简体中文](ai_zh.md)

yovoice can optionally connect to a large language model. With an AI service set up, the built-in AI assistant handles both **AI compose** in Score projects (a whole arrangement) and **AI lyrics** in Music projects (style, lyrics and title), and results can be revised further after applying them. Everything else works without it.

## Set up a service

Open Settings › AI services. The left column lists added and available services; select one and fill it in on the right:

| Service | Address | Key |
| --- | --- | --- |
| DeepSeek, Qwen (Bailian), GLM, Kimi, Doubao (Ark), MiniMax, OpenAI, Anthropic, OpenRouter, OrcaRouter | Built in | Required; **Get an API key from …** opens the provider's console |
| Ollama, LM Studio | Defaults to this computer (`127.0.0.1`), editable | Usually not needed |
| Custom | Any compatible address | Optional |

OrcaRouter has free models and needs no key to copy: click **Connect OrcaRouter for free** (on the OrcaRouter page in settings), sign in to OrcaRouter in your browser and approve. yovoice receives the key on this computer (`127.0.0.1`), adds the service with the free model `orcarouter/free` and uses it right away. Free models can be rate limited at busy times; wait a minute and retry, or pick another model.

A custom service chooses one of three protocols: OpenAI Chat Completions, OpenAI Responses or Anthropic Messages, plus the path of its model list (for example `/models`; leave it empty if the service has none and type the model name).

Type a model name, or **Sync model list** and pick one. After **Add service** or **Save changes**, **Test** sends a short message to check the address, key and model. The first service you add is used; with several, turn on **Use for AI features** on the one you want. A saved key shows only as a mask; **Replace key** or **Clear key** changes it.

## Where data goes

- Requests go straight from this computer to the service you chose, through the proxy set in Settings › General; without a configured proxy the system proxy is used. Local addresses skip the proxy.
- Keys are stored in a private table of the data folder's database (`yovoice.db`, owner-only permissions `0600`). They are never included in the state sent to the interface. The settings page only shows a masked key. Deleting a service removes its key.
- What is sent is the request you type plus a fixed instruction describing the score or lyrics format. Error messages are cleaned of keys before they are shown or logged.

## AI compose

In a Score project, **AI compose** asks for a description and a length (10–600 seconds). The built-in AI assistant writes an arrangement in the [score format](score.md#format) and submits it for validation, correcting anything the checks reject (for example a note outside its bar). The result replaces the current arrangement, so you can then adjust tracks and render as usual; after applying it, the **keep revising** input at the top lets the assistant continue on the current arrangement. Larger models give noticeably better arrangements; small local models may fail the check.

## AI lyrics

In a Music project, **AI lyrics** asks for a theme and fills in the style description, lyrics with section markers and the singing language. With Instrumental on, it writes only the style. The title is replaced only while it is still the default. Applied results can likewise be revised from the input at the top.

## Limits

- One AI request runs at a time and can be cancelled; a request stops after 10 minutes by default, adjustable in settings.
- The browser preview can edit the service list but does not call models.
- AI features have not been verified against every listed provider; if a service rejects a request, the error shows its HTTP status and message.

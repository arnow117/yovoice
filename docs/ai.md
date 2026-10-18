# AI service

**English** | [简体中文](ai_zh.md)

yovoice can optionally connect to a large language model. With an AI service set up, the **assistant panel** in the bottom-right corner chats about your work and acts on it: it creates works, writes arrangements and lyrics, edits text, assigns voices and starts generation, with every change applied in the app immediately. Everything else works without it.

## Set up a service

Open Settings › AI services. The left column lists added and available services; select one and fill it in on the right:

| Service | Address | Key |
| --- | --- | --- |
| Pay-as-you-go APIs: DeepSeek, Qwen (Bailian), GLM, Kimi, Doubao (Ark), MiniMax, OpenAI, Anthropic, OpenRouter, OrcaRouter | Built in | Required; **Get an API key from …** opens the provider's console |
| Coding plans: Kimi Code, GLM Coding Plan, Bailian Token Plan, MiniMax Token Plan, Volcengine Ark Coding Plan | Built in (the plan's own address) | Required; use the plan's key, not a pay-as-you-go API key |
| Ollama, LM Studio | Defaults to this computer (`127.0.0.1`), editable | Usually not needed |
| ChatGPT (Codex) | The Codex CLI on this computer | None; sign in with ChatGPT through Codex |
| Custom | Any compatible address | Optional |

OrcaRouter has free models and needs no key to copy: click **Connect OrcaRouter for free** (on the OrcaRouter page in settings), sign in to OrcaRouter in your browser and approve. yovoice receives the key on this computer (`127.0.0.1`), adds the service with the free model `orcarouter/free` and uses it right away. Free models can be rate limited at busy times; wait a minute and retry, or pick another model.

**ChatGPT (Codex)** uses your ChatGPT plan through the official [Codex CLI](https://github.com/openai/codex) (`npm i -g @openai/codex` or `brew install codex`). Settings shows whether Codex is installed and signed in; **Sign in with ChatGPT** runs `codex login`, which finishes in your browser, then select **Check**. yovoice never reads or stores your sign-in. Each request runs `codex exec` in an empty temporary folder with a read-only sandbox and ignores your `~/.codex/config.toml`, so models, MCP servers and hooks you set up for coding don't apply. Saving the service loads Codex's model list and picks the first model. Requests take several seconds longer than an API call and count toward your plan's usage limits.

A custom service chooses one of three protocols: OpenAI Chat Completions, OpenAI Responses or Anthropic Messages, plus the path of its model list (for example `/models`; leave it empty if the service has none and type the model name).

Type a model name, or **Sync model list** and pick one. After **Add service** or **Save changes**, **Test** sends a short message to check the address, key and model. The first service you add is used and marked **In use** in the list; with several, switch service and model from the model menu at the bottom left of the compose and lyrics bars. A saved key shows only as a mask; **Replace key** or **Clear key** changes it.

## Where data goes

- Requests go straight from this computer to the service you chose, through the proxy set in Settings › General; without a configured proxy the system proxy is used. Local addresses skip the proxy.
- Keys are stored in a private table of the data folder's database (`yovoice.db`, owner-only permissions `0600`). They are never included in the state sent to the interface. The settings page only shows a masked key. Deleting a service removes its key.
- What is sent is the request you type plus a fixed instruction describing the score or lyrics format. Error messages are cleaned of keys before they are shown or logged.

## Assistant panel

Open the panel with the assistant button in the title bar or **⌘J / Ctrl+J**. Type what you want — “create a score work and write a four-bar piano melody”, “assign a voice to each character in this story”, “generate the narration” — and the assistant does it in the app: works are created and opened, editors update as it writes, and generation starts through the same commands the app uses. Tool calls show up as cards in the conversation; generated audio can be played right there and replies can be copied. Messages sent while a turn is running are queued. **History** organizes chats into sessions you can create, switch between and delete — each session keeps its own context, up to 50 sessions of 200 messages.

Each message carries where you are (page, open work, interface language) so the assistant acts on the right target. It has no delete, download, import or settings tools — when a model is missing it tells you where to install it.

## AI compose and AI lyrics

In Score and Music projects, the **AI compose** / **AI lyrics** bar at the top sends your description into the same conversation and opens the panel: the assistant reads the work, writes the arrangement (validated like a hand-written one, with self-correction) or the style, lyrics and title, and applies it to the work directly — ask for changes in the panel to keep revising. Larger models give noticeably better arrangements; small local models may fail the check.

## Limits

- One conversation turn runs at a time and can be stopped; a turn stops after 10 minutes by default, adjustable in settings. Sessions survive restarts.
- The browser preview simulates the conversation; real model calls need the desktop app.
- AI features have not been verified against every listed provider; if a service rejects a request, the error shows its HTTP status and message.

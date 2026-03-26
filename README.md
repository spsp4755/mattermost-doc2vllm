# Mattermost LLM

Mattermost LLM is a generic Mattermost plugin for OpenAI-compatible inference endpoints such as vLLM deployments running Qwen, MiniMAX, and other text or multimodal models.

It supports:

- Text-only chat bots for normal question answering and summarization
- Multimodal bots for image and document understanding
- OCR-style extraction bots for faithful document transcription
- Follow-up thread conversations after the first bot response
- Local extraction for searchable PDFs, DOCX, XLSX, and PPTX files
- Offline-friendly plugin bundles for closed-network Mattermost deployments

## Main Capabilities

- Multiple Mattermost bot accounts managed from one plugin
- Per-bot model, mode, prompt, decoding, auth, and endpoint overrides
- Text-generation bots that work even when no file is attached
- Multimodal bots that can analyze images with OpenAI-compatible `image_url` requests
- Optional secondary vLLM post-processing for OCR/document workflows
- Access control by user, team, and channel
- Streaming responses when the target endpoint supports them

## Bot Modes

- `chat`: text generation and generic assistant workflows
- `multimodal`: image and visual-document analysis
- `ocr`: faithful document extraction workflows

## Request Shape

The plugin sends OpenAI-compatible chat completion requests. Text bots use plain `messages[].content` strings, while multimodal bots use `messages[].content[]` parts with `text` plus `image_url`.

Example multimodal request:

```json
{
  "model": "Qwen/Qwen2.5-VL-7B-Instruct",
  "messages": [
    {
      "role": "system",
      "content": "You are a multimodal AI assistant."
    },
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "Summarize the attached page."
        },
        {
          "type": "image_url",
          "image_url": {
            "url": "data:image/png;base64,..."
          }
        }
      ]
    }
  ],
  "temperature": 0,
  "max_tokens": 2048,
  "top_p": 1
}
```

Example text-only request:

```json
{
  "model": "Qwen/Qwen2.5-7B-Instruct",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful AI assistant."
    },
    {
      "role": "user",
      "content": "Summarize today's standup in 5 bullet points."
    }
  ]
}
```

## Configuration Model

The plugin stores its main settings inside the `Config` JSON field.

```json
{
  "service": {
    "base_url": "http://localhost:8000/v1/chat/completions",
    "auth_mode": "bearer",
    "auth_token": "YOUR_API_KEY",
    "allow_hosts": "localhost"
  },
  "runtime": {
    "default_timeout_seconds": 30,
    "enable_streaming": true,
    "streaming_update_ms": 800,
    "max_input_length": 4000,
    "max_output_length": 8000,
    "pdf_raster_dpi": 200,
    "max_pdf_pages": 20,
    "enable_debug_logs": false,
    "enable_usage_logs": true
  },
  "bots": [
    {
      "username": "mm-llm-chat",
      "display_name": "Mattermost LLM Chat",
      "model": "Qwen/Qwen2.5-7B-Instruct",
      "mode": "chat",
      "output_mode": "markdown",
      "ocr_prompt": "Answer clearly and keep the response concise.",
      "temperature": 0.2,
      "max_tokens": 2048,
      "top_p": 1
    },
    {
      "username": "mm-llm-qwen-vl",
      "display_name": "Mattermost LLM Vision",
      "model": "Qwen/Qwen2.5-VL-7B-Instruct",
      "mode": "multimodal",
      "output_mode": "markdown",
      "ocr_prompt": "Answer using only the visible content of the attachment.",
      "temperature": 0,
      "max_tokens": 3072,
      "top_p": 1
    }
  ]
}
```

## Closed-Network Deployment Notes

- The built plugin bundle is self-contained and does not require internet access at runtime.
- Closed-network use still requires an internally reachable OpenAI-compatible endpoint.
- Searchable PDF and Office extraction work locally on the Mattermost plugin host.
- For scanned PDFs, install one of `pdftoppm`, `mutool`, `magick`, `gswin64c`, `gswin32c`, or `gs` on the plugin host.
- When shipping into an offline environment, move only the built `dist/*.tar.gz` plugin bundle plus any required local PDF tools.

## Development

Server tests:

```bash
go test ./server/...
```

Webapp type check:

```bash
cd webapp
npm run check-types
```

Webapp tests:

```bash
cd webapp
npm test -- --runInBand
```

Full plugin bundle:

```bash
make dist
```
